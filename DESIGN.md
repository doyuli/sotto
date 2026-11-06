# Design

## Brand

Sotto is a quiet, precise Windows utility: it stays out of the way, shows the answer immediately, and makes comparison effortless. The reusable brand system is a neutral surface ladder with a single blue signal color, system UI type, soft 12–14 px corners, and one speech-bubble S mark.

- **Mark**: the app icon is an ink rounded tile holding a blue speech bubble and a white S; the in-app mark repeats that bubble and S at 20 px. It is the only decorative brand element; do not pair it with a wordmark inside the panel. Icon colors: ink `#1B2436`, blue `#4C7DF0`, S `#F7FAFF`; the in-app mark uses the accent for the bubble and the panel color for the S.
- **Color**: the blue accent belongs to the mark, the detected-language word, and focus rings. Everything else is a neutral: panel, recessed control, card title strip, card body. Never use flags, third-party service logos, or a browser-opening icon.
- **Type**: one system sans. Compact steps only: 15 px result text, 14 px input, 13 px labels, 12 px metadata. Line height 1.55–1.65 for Chinese.
- **Shape**: panel 14 px, controls and cards 12 px, icon buttons 6 px. Elevation is one soft panel shadow; nested borders are not part of the language.
- **Icons**: 2 px stroke, 16 px in controls, muted by default, stronger on hover. No emoji substitute for an icon.
- **Voice**: direct Simplified Chinese; controls name their action; errors name the problem and the recovery.

### Applying the brand

- Start from the panel and three recess levels in the ladder; keep the card-title strip visually deeper than the card body.
- Use the speech-bubble S as the only brand ornament. Do not add a wordmark or a second decorative icon inside app chrome.
- Give the accent one job per surface: brand mark, selected/detected state, or focus. Never tint a whole region with it.
- Use 2 px stroke icons at 16 px; never substitute emoji or third-party service marks.
- Ship light and dark from the same token names, not two hand-tuned palettes.

## Bubble surface

Operate. The translation bubble is a frameless 480 px Windows panel that follows the OS light/dark setting. It applies the brand system above: a white or near-black panel, a recessed composer and language bar, and cards whose title strip is deeper than their body.

## Surface ladder

The panel is the lightest layer; controls recess below it; card title strips sit between the card body and the panel.

| Token | Light | Dark | Used by |
|---|---|---|---|
| `--panel` | `#ffffff` | `#222325` | bubble panel, top bar |
| `--composer` | `#f4f4f4` | `#303132` | composer block |
| `--control` | `#f1f1f1` | `#2c2d2f` | language bar |
| `--card-head` | `#f1f1f1` | `#2c2d2f` | card title strip |
| `--card-body` | `#f6f6f6` | `#303132` | card body |
| `--pill` | `#e7e7e7` | `#3a3b3d` | detected-language badge |
| `--hover` | `#e9e9e9` | `#3a3b3d` | icon and field hover |
| `--fg` | `#202020` | `#f3f4f5` | primary text |
| `--muted` | `#6b6b6b` | `#a0a3a7` | secondary text, icons |
| `--accent` | `#2f7fe8` | `#7db4ff` | brand mark, focus ring |
| `--accent-text` | `#1a5fae` | `#7db4ff` | detected-language text |
| `--clear` | `#c9c9c9` | `#55575b` | clear button |

## Settings surface

The settings window scopes its own ladder on `body[data-route="settings"]`; the token names stay the same in both schemes, only the values flip.

| Token | Light | Dark | Used by |
|---|---|---|---|
| `--panel` | `#ffffff` | `#222325` | window body |
| `--sidebar` | `#fbfcfd` | `#1a1b1d` | navigation rail |
| `--card` | `#ffffff` | `#26272a` | cards and drawers |
| `--card-head` | `#f8f9fb` | `#2b2c30` | service title strip |
| `--field` | `#f4f5f7` | `#1e1f22` | inputs, quota well, security pill |
| `--control` | `#f1f1f1` | `#2c2d2f` | active nav, secondary buttons |
| `--pill` | `#e7e7e7` | `#3a3b3d` | quiet badges, usage track |
| `--badge-ink` | `#5c5c5c` | `#b4b7bb` | quiet badge and order-number text |
| `--success` | `#15803d` | `#4ade80` | connection-test success text |
| `--warning` / `--warning-soft` | `#b45309` / 12% | `#fbbf24` / 14% | unconfigured badge |
| `--on-accent` | `#ffffff` | `#0d1b2b` | label on an accent button |

The title strip is a quiet band, not a second surface: one step deeper than the card in light, one step lighter in dark. Badges are tint-only, with no border, so they never pick up a light hairline in dark mode.

## Type and shape

- System UI sans; input 14 px / 1.55, card text 15 px / 1.65, labels 13 px, badge 12 px.
- Radii: bubble 14 px, composer/language bar/cards 12 px, icons 6 px.
- Elevation: one soft panel shadow; cards use the surface ladder instead of borders.
- Icons: 2 px stroke, 16 px in controls, muted by default, `--icon-strong` on hover.

## Components

- **Top bar**: small brand sparkle at the left; pin/close at the right. Pinned bubbles drag by the whole bar.
- **Composer**: textarea plus an inline row of speak, copy, detected-language badge, and clear. The badge is hidden until a concrete language is known.
- **Language bar**: one control with source select, swap, target select. Carets are drawn; source supports automatic detection or a manual language. No flags.
- **Result cards**: deeper title strip, lighter body, collapse chevron (down expanded, left collapsed), text scrolls at 220 px, tools row with naming-style icons then speak/copy.
- **States**: skeleton lines for loading, red text plus retry for errors, empty composer hides the cards; focus rings use the accent.

## Motion

150 ms state transitions only: hover, collapse, badge/state changes. Shimmer runs only while loading. `prefers-reduced-motion` disables both.
