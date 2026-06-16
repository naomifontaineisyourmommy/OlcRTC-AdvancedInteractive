# olcrtc patch spec: WB Stream owner mode for `srv` (create + own)

> **Назначение этого файла.** Это спецификация одной изолированной правки для
> olcrtc. Скорми ИИ-ассистенту **чистый (актуальный) исходник olcrtc вместе с
> этим файлом** и попроси применить правку. Цель — единственная: дать серверному
> пиру (`mode: srv`) возможность работать на **WB Stream как владелец комнаты по
> bearer-токену аккаунта** — при необходимости самому создав комнату.
>
> Токен задаётся **одним опциональным инлайн-полем `auth.token`** в YAML-конфиге.
> Никакого отдельного файла токена и никакого CLI-флага.
>
> **Вне области правки (НЕ добавлять):** режим `gen`, любая работа с
> Telemost/cookie, refresh-токены, персист roomId между перезапусками,
> token-файл, CLI-параметры. Telemost и прочие провайдеры не трогать.

---

## 1. Зачем это нужно (контекст для ИИ)

Комната WB Stream, созданная через API, **не существует на медиа-уровне
(LiveKit), пока в неё не подключится владелец**. Гость, запрашивающий
`GET /api-room-manager/v2/room/{id}/connection-details`, получает
`403 {"code":7,"message":"guests cannot create rooms"}`, пока в комнате нет
подключённого владельца. Только когда участник с токеном-владельцем реально
подключается к LiveKit, комната «оживает» и гости проходят.

В тоннеле olcrtc два пира (`srv` и `cnc`) встречаются в одной комнате. В чистом
olcrtc оба заходят гостями (`wbstream.Provider.Issue` всегда делает
`registerGuest → joinRoom → getToken`), поэтому никто не открывает комнату.

Правка делает так, что **`srv` заходит ВЛАДЕЛЬЦЕМ** по токену аккаунта (при
необходимости сам создав комнату) и держит её открытой весь срок своей работы, а
**`cnc` остаётся гостём**. Подтверждено сквозным запуском: srv с токеном создаёт
комнату и поднимает owner-соединение к `wss://rtc-el-01.wb.ru`, после чего
гостевой `connection-details` начинает возвращать `200` (до этого — `403`).

---

## 2. Контракт поведения

| `auth.token` | `room.id` | поведение |
|---|---|---|
| задан | пуст | srv **создаёт** комнату по токену, владеет ею; печатает в stdout строку `Created and connected to WB Stream room id: <room id>` |
| задан | указан | srv подключается к **указанной** комнате как владелец (создание не выполняется, в stdout ничего не печатается) |
| пуст | указан | поведение чистого olcrtc (гость) |
| пуст | пуст | как сейчас — ошибка валидации `room.id required` |

Правила:

- **Наличие токена ⇒ owner-режим** в `Issue`: токен используется напрямую как
  access-токен (без `registerGuest`/`joinRoom`), затем `connection-details`.
- **Создание комнаты — только когда токен задан И `room.id` пуст**, и **ровно
  один раз при старте процесса srv** (не на каждом переподключении внутри
  сессии). На рестарт процесса допускается новая комната с новым id — это
  приемлемо.
- Токен пробрасывается **только по серверному пути** (`mode: srv`). Клиентский
  путь (`internal/client/client.go`) токен не получает, поэтому `cnc` всегда
  гость. Это и разграничивает роли.
- Печать строки `Created and connected to WB Stream room id: <room id>` —
  **только** в случае token+пустой room.id (когда srv реально создал комнату).
  Формат — голый stdout (логи olcrtc идут в stderr, stdout остаётся чистым для
  парсинга внешними скриптами).

Откуда оператор берёт токен: из залогиненной сессии stream.wb.ru → DevTools →
Network → любой запрос к `stream.wb.ru/api-room` → заголовок
`Authorization: Bearer <token>`. Токен долгоживущий (WBID JWT без `exp`).

---

## 3. Изменения по файлам

Пути относительно корня репозитория. Имена структур/функций соответствуют текущей
кодовой базе olcrtc; точки вставки описаны явно.

### 3.1. `internal/auth/auth.go` — поле в `auth.Config`

Добавить в структуру `Config` (рядом с `Name`/`RoomURL`):

```go
// AccountToken is a service account bearer token (e.g. a WB Stream access
// token). When set, a provider that supports owner mode (wbstream) connects
// as the room owner instead of a guest, and (via RoomCreator) can create a
// room. Optional; only consumed by providers that document it.
AccountToken string
```

### 3.2. `internal/auth/wbstream/api.go` — функция создания комнаты

Добавить ошибки, типы и функцию `createRoom` (HTTP `POST /api-room/api/v2/room`).
В блок объявления ошибок пакета (`errGuestRegister`, ...) добавить:

```go
errCreateRoom = errors.New("create room failed")
// ErrTokenRequired is returned when room creation is attempted without an account token.
ErrTokenRequired = errors.New("wbstream: account token required for room creation")
```

И сами типы + функцию:

```go
type createRoomRequest struct {
	RoomType    string `json:"roomType"`
	RoomPrivacy string `json:"roomPrivacy"`
}

type createRoomResponse struct {
	RoomID string `json:"roomId"`
}

// createRoom creates a new room on the authenticated account identified by
// accessToken (a WB Stream bearer token) and returns its room ID.
func createRoom(ctx context.Context, accessToken string) (string, error) {
	u := apiBase + "/api-room/api/v2/room"
	reqBody := createRoomRequest{
		RoomType:    "ROOM_TYPE_ALL_ON_SCREEN",
		RoomPrivacy: "ROOM_PRIVACY_FREE",
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux x86_64)")

	client := protect.NewHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("create room status: %w", protect.StatusError(errCreateRoom, resp, 4096))
	}

	var res createRoomResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if res.RoomID == "" {
		return "", fmt.Errorf("create room: %w: empty room id", errCreateRoom)
	}
	return res.RoomID, nil
}
```

> Примечание для ИИ: `apiBase`, `protect.NewHTTPClient`, импорты `bytes`,
> `encoding/json`, `errors`, `fmt`, `net/http` уже есть в пакете — переиспользовать.

### 3.3. `internal/auth/wbstream/wbstream.go` — owner-ветка в `Issue` + `CreateRoom`

Заменить метод `Issue` на версию с owner-веткой (при заданном `AccountToken`
токен используется напрямую, без `registerGuest`/`joinRoom`):

```go
// Issue runs the WB Stream auth flow and returns LiveKit credentials.
//
// When cfg.AccountToken is set the caller authenticates as the room owner: the
// account token is used directly to fetch the room connection token. This is
// how an olcrtc server opens and holds a room so guest clients can join (WB
// Stream rooms only accept guests once an owner is connected). Otherwise a
// guest is registered and joined - the path olcrtc clients take.
func (Provider) Issue(ctx context.Context, cfg auth.Config) (auth.Credentials, error) {
	if cfg.RoomURL == "" || cfg.RoomURL == "any" {
		return auth.Credentials{}, auth.ErrRoomIDRequired
	}

	roomID := cfg.RoomURL
	accessToken := cfg.AccountToken
	if accessToken == "" {
		guest, err := registerGuest(ctx, cfg.Name)
		if err != nil {
			return auth.Credentials{}, fmt.Errorf("register guest: %w", err)
		}
		if err := joinRoom(ctx, guest, roomID); err != nil {
			return auth.Credentials{}, fmt.Errorf("join room: %w", err)
		}
		accessToken = guest
	}

	tok, err := getToken(ctx, accessToken, roomID, cfg.Name)
	if err != nil {
		return auth.Credentials{}, fmt.Errorf("get token: %w", err)
	}

	url := tok.ServerURL
	if url == "" {
		url = defaultWSURL
	}

	return auth.Credentials{
		URL:   url,
		Token: tok.RoomToken,
		Extra: map[string]string{"roomID": roomID},
	}, nil
}

// CreateRoom creates a new WB Stream room on the account identified by
// cfg.AccountToken and returns the room ID. Implements auth.RoomCreator so the
// session layer can mint a room for an owner-mode server at startup.
func (Provider) CreateRoom(ctx context.Context, cfg auth.Config) (string, error) {
	if cfg.AccountToken == "" {
		return "", ErrTokenRequired
	}
	roomID, err := createRoom(ctx, cfg.AccountToken)
	if err != nil {
		return "", fmt.Errorf("create room: %w", err)
	}
	return roomID, nil
}
```

> `registerGuest`, `joinRoom`, `getToken`, `defaultWSURL`, `tokenResponse` уже
> существуют в `api.go` — переиспользовать.

### 3.4. `internal/config/config.go` — опциональное YAML-поле `auth.token`

Токен задаётся **одним опциональным инлайн-полем `auth.token`** в YAML-конфиге.
Никакого отдельного файла токена и загрузчиков — только поле и его маппинг в
`session.Config`.

В структуру `Auth` добавить поле:

```go
// Token is an optional service account bearer token used by providers that
// connect as room owner (e.g. a WB Stream access token for wbstream owner
// mode). It is set directly in the config; there is no separate token file.
Token string `yaml:"token"`
```

В функции `Apply` (рядом с `dst.Auth = pickString(dst.Auth, f.Auth.Provider)`)
добавить маппинг поля в `session.Config`:

```go
dst.AccountToken = pickString(dst.AccountToken, f.Auth.Token)
```

> Это всё для config.go. Никаких ошибок, файловых загрузчиков или правок
> `loadExternalSecrets` добавлять НЕ нужно — токен живёт прямо в YAML.

### 3.5. `internal/app/session/session.go` — поле, валидация, создание при старте

**(a)** В `session.Config` добавить поле (рядом с `Auth`):

```go
AccountToken string
```

**(b)** Ослабить валидацию `room.id` в `validateCommon`. Заменить:

```go
if cfg.RoomID == "" && cfg.Auth != authNone {
	return ErrRoomIDRequired
}
```

на:

```go
// An owner-mode wbstream server (mode srv + account token) creates its room
// at startup, so room.id may be omitted in that case.
if cfg.RoomID == "" && cfg.Auth != authNone && !(cfg.Mode == modeSRV && cfg.AccountToken != "") {
	return ErrRoomIDRequired
}
```

**(c)** Создание комнаты при старте srv. В функции `Run`, сразу после строки
`roomURL := cfg.RoomID` (и после `configureDefaultResolver(...)`, который уже
вызывается выше — резолвер должен быть настроен до HTTP-запроса), добавить:

```go
if cfg.Mode == modeSRV && cfg.AccountToken != "" && roomURL == "" {
	id, err := createOwnerRoom(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create owner room: %w", err)
	}
	fmt.Fprintf(os.Stdout, "Created and connected to WB Stream room id: %s\n", id)
	roomURL = id
}
```

И добавить хелпер (рядом с `Gen`/`Run`):

```go
func createOwnerRoom(ctx context.Context, cfg Config) (string, error) {
	p, err := auth.Get(cfg.Auth)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedCarrier, cfg.Auth)
	}
	creator, ok := p.(auth.RoomCreator)
	if !ok {
		return "", fmt.Errorf("%w: %s does not support room creation", ErrUnsupportedCarrier, cfg.Auth)
	}
	return creator.CreateRoom(ctx, auth.Config{
		Name:         names.Generate(),
		AccountToken: cfg.AccountToken,
		DNSServer:    cfg.DNSServer,
	})
}
```

> Примечания для ИИ:
> - Создание выполняется здесь, в `Run`, **один раз** — переменная `roomURL`
>   затем передаётся в `runOnce`/цикл ротации, поэтому owner-переподключения и
>   плановые ротации внутри процесса идут к уже созданному id, не пересоздавая
>   комнату. Новая комната появится только при перезапуске процесса.
> - Строка печатается через `os.Stdout` (как room id в режиме `gen`), чтобы
>   внешние скрипты могли её распарсить; обычные логи olcrtc идут в stderr.
>   Убедиться, что пакеты `os` и `fmt` импортированы в `session.go` (`fmt`
>   обычно уже есть; `os` — добавить при отсутствии).
> - `auth`, `names`, `ErrUnsupportedCarrier`, `modeSRV` уже доступны в пакете.
> - Строка «Created and connected ...» печатается сразу после создания комнаты
>   (непосредственно перед установкой owner-соединения, которое выполняется ниже
>   в `server.Run`).

**(d)** В `runOnce`, ветка `case modeSRV:`, в литерале `server.Config{...}`
добавить проброс токена (рядом с `Token: cfg.Token,`):

```go
AccountToken: cfg.AccountToken,
```

> НЕ добавлять `AccountToken` в `client.Config{...}` (ветка `modeCNC`).

### 3.6. `internal/server/server.go` — поле и проброс в транспорт

В `server.Config` (рядом с `Token`):

```go
// AccountToken is a service account bearer token. When set, the carrier's
// auth provider connects as the room owner instead of a guest (wbstream).
AccountToken string
```

В месте, где строится `transport.Config{...}` (напр. `bringUpLink`), рядом с
`Token: cfg.Token,`:

```go
AccountToken: cfg.AccountToken,
```

### 3.7. `internal/transport/transport.go` — поле в `transport.Config`

В `Config` (рядом с `Token`):

```go
// AccountToken is a service account bearer token forwarded to the auth
// provider so it connects as room owner rather than guest (wbstream).
AccountToken string
```

### 3.8. `internal/engine/builtin/builtin.go` — поле и проброс в `auth.Config`

В `Config` (рядом с `Token`):

```go
// AccountToken is forwarded to the auth provider's Issue. When set, a
// provider that supports owner mode (wbstream) connects as room owner.
AccountToken string
```

В `registerEngineAuth`, в литерале `auth.Config{...}`:

```go
AccountToken: cfg.AccountToken,
```

> `registerDirect` (carrier `none`) НЕ трогать.

### 3.9. Транспорты — проброс в `enginebuiltin.Config`

В КАЖДОМ из четырёх файлов, в вызове
`enginebuiltin.Open(ctx, cfg.Carrier, enginebuiltin.Config{...})`, рядом с
`Token: cfg.Token,` добавить:

```go
AccountToken: cfg.AccountToken,
```

Файлы:
- `internal/transport/datachannel/transport.go`
- `internal/transport/seichannel/transport.go`
- `internal/transport/videochannel/transport.go`
- `internal/transport/vp8channel/transport.go`

### 3.10. `internal/client/client.go` — НЕ ИЗМЕНЯТЬ

`cnc` всегда гость; токен ему не пробрасывается.

---

## 4. Тесты

### 4.1. Обязательная правка существующего теста

После добавления `wbstream.CreateRoom` провайдер `wbstream` начинает
реализовывать `auth.RoomCreator`. Из-за этого **ломается существующий тест**
`TestValidateGen` в `internal/app/session/session_test.go`: в нём есть кейс,
утверждающий, что wbstream НЕ умеет создавать комнаты (ждёт
`ErrUnsupportedCarrier`). Его нужно перевести на провайдера, который
действительно не реализует `auth.RoomCreator` — например `jitsi`:

```go
// было:
{
	name: "wbstream room generation unsupported",
	cfg:  Config{Auth: testAuthWBStream, DNSServer: "8.8.8.8:53", Amount: 3},
	want: ErrUnsupportedCarrier,
},
// стало:
{
	name: "jitsi room generation unsupported",
	cfg:  Config{Auth: "jitsi", DNSServer: "8.8.8.8:53", Amount: 3},
	want: ErrUnsupportedCarrier,
},
```

> Прочие кейсы того же теста, использующие `testAuthWBStream` (missing dns,
> amount 0/негативный), править НЕ нужно — они срабатывают раньше проверки
> `RoomCreator` и остаются валидными. Если в актуальной версии аналогичный кейс
> завязан на другой провайдер, поступить так же: заменить на `jitsi`.
>
> Без этой правки `go test ./internal/app/session/` упадёт; `go build ./...`
> при этом проходит (правка нужна именно для тестов).

### 4.2. Новые тесты (рекомендуется добавить)

В `internal/auth/wbstream/api_test.go` (тесты пакета подменяют `apiBase` на
`httptest`-сервер через хелпер `withWBAPIServer`):

```go
func TestWBStreamCreateRoom(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api-room/api/v2/room", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+testAccessToken {
			t.Fatalf("Authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(createRoomResponse{RoomID: testRoomID})
	})
	withWBAPIServer(t, mux)

	roomID, err := Provider{}.CreateRoom(context.Background(), auth.Config{AccountToken: testAccessToken})
	if err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}
	if roomID != testRoomID {
		t.Fatalf("CreateRoom() = %q, want %q", roomID, testRoomID)
	}
}

func TestWBStreamCreateRoomRequiresToken(t *testing.T) {
	if _, err := (Provider{}).CreateRoom(context.Background(), auth.Config{}); !errors.Is(err, ErrTokenRequired) {
		t.Fatalf("CreateRoom(no token) error = %v, want %v", err, ErrTokenRequired)
	}
}

func TestWBStreamIssueOwner(t *testing.T) {
	const ownerToken = "owner-account-token"
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/api/v1/auth/user/guest-register", func(http.ResponseWriter, *http.Request) {
		t.Fatal("owner path must not register a guest")
	})
	mux.HandleFunc("POST /api-room/api/v1/room/{id}/join", func(http.ResponseWriter, *http.Request) {
		t.Fatal("owner path must not call join")
	})
	mux.HandleFunc("GET /api-room-manager/v2/room/{id}/connection-details", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+ownerToken {
			t.Fatalf("Authorization = %q, want owner token", got)
		}
		_ = json.NewEncoder(w).Encode(tokenResponse{RoomToken: testToken, ServerURL: "wss://rtc.example"})
	})
	withWBAPIServer(t, mux)

	creds, err := Provider{}.Issue(context.Background(), auth.Config{
		RoomURL:      testRoomID,
		Name:         testPeerName,
		AccountToken: ownerToken,
	})
	if err != nil {
		t.Fatalf("Issue(owner) error = %v", err)
	}
	if creds.Token != testToken || creds.URL != "wss://rtc.example" {
		t.Fatalf("creds = %+v", creds)
	}
}
```

> Константы `testAccessToken`, `testRoomID`, `testToken`, `testPeerName`, тип
> `tokenResponse` и хелпер `withWBAPIServer` уже есть в тестах пакета —
> переиспользовать; при иных именах подставить фактические. Существующие
> гостевые тесты ломаться не должны: owner-ветка включается только при заданном
> `AccountToken`.

---

## 5. Проверка после применения

```
go build ./...
go vet ./...
go test ./internal/auth/wbstream/ ./internal/config/ ./internal/app/session/ \
        ./internal/server/ ./internal/transport/... ./internal/engine/builtin/
```

Должно быть зелёным.

### Пример конфигов

> Для WB Stream транспорт должен быть **видео** (`vp8channel`; также возможны
> `videochannel`/`seichannel`). `datachannel` поверх WB Stream **не работает** —
> его SFU не разносит data-пакеты между участниками, туннель не поднимется.

```yaml
# srv.yaml — создаёт комнату и заходит ВЛАДЕЛЬЦЕМ (room.id НЕ указывать)
mode: srv
auth:
  provider: wbstream
  token: "<WB access token>"      # access-токен аккаунта (инлайн в конфиге)
net:
  transport: vp8channel
  dns: 8.8.8.8:53
vp8:
  fps: 60
  batch_size: 64
crypto:
  key: "<64 hex>"                 # общий ключ; должен совпадать с cnc
data: data
```

```yaml
# cnc.yaml — заходит ГОСТЁМ; room.id берётся из stdout srv (внешними скриптами)
mode: cnc
auth:
  provider: wbstream
room:
  id: <room id, напечатанный srv>
net:
  transport: vp8channel
  dns: 8.8.8.8:53
vp8:
  fps: 60
  batch_size: 64
crypto:
  key: "<тот же 64 hex>"
socks:
  host: 127.0.0.1
  port: 1080
data: data
```

### Ожидаемое поведение

1. `srv` стартует с токеном и без `room.id` → создаёт комнату по токену →
   печатает в stdout: `Created and connected to WB Stream room id: <uuid>` →
   подключается к LiveKit владельцем (в логах stderr — `Link connected`). Комната
   открыта.
2. Внешние скрипты читают этот id из stdout srv и кладут его в `room.id` конфигов
   `cnc`.
3. `cnc` стартует гостём → `connection-details` теперь `200` (раньше был `403`) →
   присоединяется. Тоннель устанавливается.

---

## 6. Заметки и подводные камни

- **Транспорт для WB — только видео** (`vp8channel`/`videochannel`/`seichannel`).
  `datachannel` поверх WB Stream не несёт данные (см. §5).
- **room.id + токен одновременно** → srv подключается к указанной комнате
  владельцем, новую НЕ создаёт и ничего не печатает. (Предполагается, что
  оператор, знающий о патче, при создании на лету `room.id` не указывает.)
- **Создание — раз на процесс.** При перезапуске процесса srv создаётся новая
  комната с новым id; внешние скрипты перераздадут его клиентам. Персист id не
  предусмотрен (намеренно — не входит в область правки).
- **Накопление комнат.** Каждый перезапуск процесса создаёт новую комнату на
  аккаунте; если у WB есть лимит на число комнат — теоретически можно упереться.
  Не критично, но иметь в виду.
- **Авто-refresh владельца** обеспечивается существующим механизмом: путь
  `registerEngineAuth` передаёт движку колбэк `Refresh`, который при
  переподключении повторно вызывает `provider.Issue` — а тот в owner-режиме снова
  возьмёт `AccountToken` и перевыпустит LiveKit room-token к **той же** комнате.
  Отдельная логика refresh не нужна.
- **Секрет.** `auth.token` — bearer уровня доступа к аккаунту, лежит инлайн в
  YAML-конфиге. Держи сам конфиг с правами `600`, вне VCS; не логировать.
- **WB TURN** может отдавать `401` на части ICE-кандидатов — не блокер,
  соединение встаёт по рабочему candidate-pair (наблюдалось при проверке).
- **Минимальность.** Никаких других провайдеров, режимов (`gen`) и функций не
  добавлять — только owner-режим + создание комнаты для `srv` на WB Stream.
