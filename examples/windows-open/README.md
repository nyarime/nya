# Windows: double-click `.nya` → extract beside archive

Goal: `game.nya` opens with `nya.exe` and extracts into `.\game\`.

## One-time setup

1. Install `nya.exe` on `PATH` (or note its full path).
2. Register the association for the **current user**:

```bat
nya associate
```

This writes HKCU keys so Explorer runs:

```text
"C:\path\to\nya.exe" open "%1"
```

Remove later:

```bat
nya associate -uninstall
```

## Behaviour

| Action | Result |
| --- | --- |
| Double-click `D:\Downloads\game.nya` | Creates `D:\Downloads\game\` and extracts into it |
| Folder already exists | Creates `game(2)\`, then `game(3)\`, … (WinRAR-style; 7-Zip file auto-rename is `name_1.txt`) |
| Console window | Closes when extract finishes (use `nya open -pause` to wait for Enter) |
| Scripts | `nya extract …` or `nya open …` |

Override output directory:

```bat
nya open -o D:\out\mygame game.nya
```

If `-o` / default dest already exists, `nya open` still picks `name(2)` etc. so a second double-click does not merge into the first folder.

## Manual `.reg` (optional)

If you cannot run `associate`, import a `.reg` after editing the `nya.exe` path:

```reg
Windows Registry Editor Version 5.00

[HKEY_CURRENT_USER\Software\Classes\.nya]
@="Nyarime.NYA"

[HKEY_CURRENT_USER\Software\Classes\Nyarime.NYA]
@="Nyarime Archive"

[HKEY_CURRENT_USER\Software\Classes\Nyarime.NYA\shell\open\command]
@="\"C:\\Tools\\nya.exe\" open \"%1\""
```

## Related

- Self-extracting EXE without installing `nya`: `nya sfx` / `nya create -sfx` (see [SPEC-SFX.md](../../SPEC-SFX.md)).
- Scripted extract to cwd: `nya extract archive.nya .`
