# Owner-mode fork maintenance

This fork carries one feature on top of upstream olcrtc: **WB Stream owner
mode** (see `OlcRTC.WBStream.OwnerMode.patch.md` for the spec and
`OlcRTC.WBStream.OwnerMode.README.md` for how to use it).

The change is **not** stored as a text patch. It lives as a single git commit
on the **`owner-mode`** branch. That means git's 3-way merge keeps it applying
cleanly even when upstream shifts surrounding lines — you only ever resolve a
conflict if upstream edits the exact same lines.

## Branch layout

- `master` — a pristine mirror of `upstream/master`. Never commit here.
- `owner-mode` — `master` + the owner-mode commit. This is what you build/run.

Remotes (already configured):

- `origin`   → your fork (`naomifontaineisyourmommy/OlcRTC-AdvancedInteractive`)
- `upstream` → `openlibrecommunity/olcrtc`

## Updating to a new upstream release

One command:

```powershell
./script/update-owner-mode.ps1
```

It fetches upstream, rebases `owner-mode` onto `upstream/master`, then runs
`go build ./...` and `go test ./...` so you instantly know it's still good.

If it reports a conflict, open the listed files, fix the `<<<<<<<` markers,
then:

```powershell
git add <fixed-files>
git rebase --continue
```

…or back out with `git rebase --abort`. When it finishes green, publish:

```powershell
git push --force-with-lease origin owner-mode
```

(The force is expected: rebasing rewrites the branch's base. `--force-with-lease`
refuses to overwrite anything you didn't fetch, so it's safe for a solo fork.)

## Doing it by hand (what the script runs)

```powershell
git fetch upstream
git checkout owner-mode
git rebase upstream/master
go build ./... ; go test ./...
```

## Keeping master as a clean mirror (optional)

```powershell
git checkout master
git merge --ff-only upstream/master
git push origin master
git checkout owner-mode
```
