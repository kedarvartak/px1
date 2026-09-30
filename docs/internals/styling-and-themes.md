# Styling & CSS Tokens

px1 uses one fixed GitHub Dark palette so the review surface stays predictable
across machines. The palette is embedded in
[`web/themes/github-dark.css`](../../web/themes/github-dark.css) and linked
directly by [`web/index.html`](../../web/index.html); there is no runtime theme
discovery, preference, or switching path.

`web/style.css` owns layout and component styling. Components consume CSS
custom properties rather than repeating colors, which keeps review states
consistent without carrying a theme system through the application.

## Token contract

The palette supplies the tokens used by the editor, diff viewer, review queue,
and overlays.

| Group | Tokens |
| --- | --- |
| Surfaces | `--bg`, `--bg2`, `--bg3`, `--bg4` |
| Text | `--fg`, `--dim`, `--faint` |
| Borders and accents | `--line`, `--accent`, `--accent-fg` |
| Selection and search | `--sel`, `--mark`, `--mark-active`, `--cur` |
| Syntax | `--k`, `--nf`, `--nc`, `--nb`, `--nv`, `--no`, `--nt`, `--nd`, `--np`, `--s`, `--m`, `--o`, `--p`, `--c`, `--err` |
| Review and status | `--gi`, `--gd`, `--gi-bg`, `--gd-bg`, `--shadow` |

The palette is deliberately kept separate from structural CSS so a future
product-level visual refresh can replace one asset without restoring a user
preference subsystem.
