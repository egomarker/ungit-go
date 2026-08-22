# External merge tools

Ungit-Go can launch a Git merge tool configured on your system.

## 1. Configure Git

Configure a merge tool in `~/.gitconfig`. Example:

```ini
[mergetool "extMerge"]
    cmd = extMergeTool "$BASE" "$LOCAL" "$REMOTE" "$MERGED"
    trustExitCode = false

[merge]
    tool = extMerge
```

Test the tool from a repository that has unresolved conflicts:

```sh
git mergetool --tool extMerge
```

## 2. Configure Ungit-Go

Ungit-Go intentionally retains the `.ungitrc` filename for compatibility. Set `mergeTool` to `true` to use Git's configured default, or set the tool name explicitly:

```json
{
  "mergeTool": "extMerge"
}
```

## 3. Use it

Open a conflicted repository in Ungit-Go. The conflicted-file controls can launch the configured merge tool. After the tool saves a resolution, Ungit-Go should detect the change; otherwise use **Mark as Resolved**.

## Notes

- Windowed merge tools work best. Terminal-only tools such as `vimdiff` are not suitable when Ungit-Go is running detached from a terminal UI.
- Launch can take a few seconds; clicking repeatedly can start multiple copies.
- `trustExitCode` behavior is controlled by Git/the merge tool, not Ungit-Go.
