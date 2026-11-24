# epi

Keeps your Epitech folder organized. It clones new projects from GitHub into the
right place, with the right name, files their subject PDFs next to them, and
tells you what needs attention.

```
~/Epitech/
  tek1/
    pool/                  day01 … day13, rush1, rush2, star, subjects/
    cpe101_c_programming/  mini_printf, my_printf, subjects/
    psu100_unix/           my_ls, my_sudo, subjects/
    mul100_graphics/       my_hunter, my_radar, subjects/
```

## Install

It comes with [epitech-toolkit](https://github.com/cempack/epitech-toolkit),
which keeps it updated. On its own, take the binary from the
[latest release](https://github.com/cempack/epi/releases/latest):

```sh
curl -fsSL -o ~/.local/bin/epi https://github.com/cempack/epi/releases/latest/download/epi-linux-amd64
chmod +x ~/.local/bin/epi
echo 'command -v epi >/dev/null && eval "$(command epi shell)"' >> ~/.bashrc
```

The last line lets plain `epi` move your shell into the project you pick.
epi needs [`gh`](https://cli.github.com) logged in (`gh auth login`) and, to
read subject titles, `pdftotext` (poppler).

## Use

| | |
|---|---|
| `epi` | Browse your projects: what's uncommitted or unpushed, the last commit, the subjects. **enter** goes into the project, `o` opens its subject, `w` opens it on GitHub, `/` filters. |
| `epi new` | Lists the Epitech repositories on your GitHub you haven't cloned yet. Pick one: it shows the plan, clones it and files its subjects from `~/Downloads`. |
| `epi new my_radar` | The same for one project: a name, a slug or the repository URL. |
| `epi doctor` | Checks names, projects in the wrong module, unsaved work, clutter, repositories not cloned, and offers to file subject PDFs left in `~/Downloads`. |

## How it decides

- **Where.** The repository name says it all: `G-PSU-100-TLS-1-1-myls-1` is
  module PSU-100 of the first year, so it goes in `tek1/psu100_unix/`. The pool
  (CPE-100) goes in `tek1/pool/`. For a module it has never seen, epi asks a
  short topic once, then reuses the folder.
- **The name.** Repository names lose their separators (`myls`, `settingup`);
  subject titles keep them (`MY_LS`, `SETTING UP`). epi reads the first page of
  the subject and keeps the words that spell the repository's name: `my_ls`,
  `setting_up`.
- **Which PDFs.** Only the ones that mention the project, or that look like an
  Epitech subject (the `< TAGLINE />` under the title): never your other
  downloads. A bootstrap becomes `my_ls-bootstrap.pdf`, the subject `my_ls.pdf`.
- **Nothing happens before you confirm.** Files are moved, never overwritten.

## Settings

| Variable | Default |
|---|---|
| `EPI_ROOT` | `~/Epitech` |
| `EPI_DOWNLOADS` | `~/Downloads` |

## Build

```sh
go build -o ~/.local/bin/epi .
go test ./...
```

Releases are built by GitHub Actions when a `v*` tag is pushed.

Unofficial: not affiliated with Epitech.
