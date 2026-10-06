# Nova Sprint artwork layers

Fresh individual renders, generated with OpenAI image generation on 2026-10-05
at Glenn's request. Every image here is a **lossless PNG**. The character and
wordmark sources retain their original transparency and resolution. The original
renders are copied byte for byte; there is no JPEG conversion in this pipeline.

[View the approved jogging banner](jogging/banner-cute-wave-left.png), or the earlier
[group composition](composite.png).

## What the friends represent

These are Glenn's character meanings for Nova Sprint. Keep them consistent
when using the individual assets in illustrations and documentation.

| Character | Meaning |
|---|---|
| [Little robot with the checklist](stella.png) | The coordinator. |
| [Sleeping yellow friend](yellow-v2.png) | Friends falling asleep when they should be working together. |
| [Green friend with headphones](green.png) | Friends who cannot hear their teammates: messages and coordination do not reach them. |
| [Tripping purple friend](purple.png) | A friend failing at their task because a dependency is blocked on another friend who has fallen asleep. |
| [Pink friend on a bike](pink-cyclist.png) | A friend having fun and moving forward at her own pace. |
| [White-and-blue runner](white.png) | A friend getting the task done correctly. |
| [White-and-blue friend on starting blocks](starting-blocks/character.png) | A fast friend ready to take the next task quickly. |
| [Orange friend](orange.png) with the [bees](bees.png) | The bees represent swarms of AIs running across the fleet. |

Purple's stumble and Yellow's sleep tell a connected dependency story. Keep
the bees associated with Orange when illustrating fleet swarms.

## Illustrated explainer

[The illustrated README](../README.md) uses every
character to explain why coordination needs machinery, then introduces
friends, swarms, fleet capacity, streams, cards, cross-stream dependencies,
sentinel gates, reviews, and landing.
The six panels in `explainer/` are deterministic compositions of the original
PNG assets, with drawn diagrams and text. Rebuild with
`python3 brand/compose_explainer.py` (Pillow); set `NOVA_BRAND_FONT` to a local
Arial-compatible TrueType font when the default macOS Arial path is unavailable.
Yellow was re-rendered separately to correct the leg anatomy; the approved
replacement is [yellow-v2.png](yellow-v2.png), with its
[prompt](yellow-v2-prompt.txt). The original remains in the archive.
The other characters use their original renders.

The README selects light and dark explainer images with GitHub's `<picture>`
support and `prefers-color-scheme`. The jogging hero stays the same in both
themes. Dark panels use a `#0d1117` background, pale labels, and dark diagram
surfaces with bright connectors. Existing light images remain the fallback.
Rebuild the four drawn dark panels with:

```sh
python3 brand/compose_explainer.py --theme dark --panels machine streams team landed
```

The README's current first panel is the separately generated
[short-leg illustration](explainer/coordination-short-legs.png), with a matching
[dark variant](explainer/coordination-short-legs-dark.png). Its
[dark edit prompt](explainer/coordination-short-legs-dark-prompt.txt) preserves
the corrected Yellow proportions. The renderer's older `coordination` panel
does not replace either of these approved images.

## Approved jogging banner

The README uses [banner-cute-wave-left.png](jogging/banner-cute-wave-left.png): the happy
white-and-blue friend jogging and waving, with robot and logo centred as a
single group, with a little extra room between the waving hand and the logo.
The mascot uses the preferred runner's rounder head and compact body.
Glenn approved this version on 2026-10-05. The built-in image
generator rendered the complete banner; the native 2171 × 724 PNG is copied
without resampling or lossy conversion. Its [final prompt](jogging/prompt-cute-wave-left.txt)
and earlier layout variants are saved alongside it.

All six explainer panels are drawn at 4× logical resolution and exported
as 2× lossless PNGs. Text, boxes, dividers, checkmarks, and curved connectors
are antialiased; original character assets are preserved.

## Starting blocks

The latest direction returns to the original mascot, freshly rendered on the
starting blocks at the left, with the flat wordmark ahead of him. The grass
strip is removed by continuing the pale background to the track edge; lane
pixels are preserved. All source and placed layers are lossless PNGs in
[starting-blocks](starting-blocks/), with the generation
[prompt](starting-blocks/prompt.txt) and placement record alongside them.

Rebuild with `python3 brand/compose_starting_blocks.py` (Pillow and NumPy).
The script also saves the previous [running version without grass](starting-blocks/running-no-grass.png).
This earlier README hero remains available for reuse.

## Solo runner

The revised [side-on version](solo-runner-v2/composite.png) uses a freshly
generated quiet track plate, with left-to-right lanes and open sky behind the
wordmark. The original character and logo are composited without regeneration.
Its lossless layers and [background prompt](solo-runner-v2/prompt.txt) are saved
in `solo-runner-v2/`. Rebuild with `python3 brand/compose_solo_v2.py` (Pillow
required). The script checks that background pixels outside the overlays remain
unchanged. These earlier options are retained alongside the selected jogging hero.

The simpler banner pairs the white-and-blue sprinter with the flat wordmark
ahead of him. It uses the original character and logo, separate shadows, and
a native-resolution crop of the stadium. No generation pass touches the
assembled image or redraws the lane lines. The 1872 × 624 PNG and all its
layers are in [solo-runner](solo-runner/).

Rebuild with Python and Pillow:

```sh
python3 brand/compose_solo.py
```

There is also a [race-banner variation](solo-runner/ribbon-composite.png)
with the wordmark on a separate white panel. These earlier options are retained for reference.

| Layer | Source |
|---|---|
| Stadium and track, original render | [track.png](track.png) |
| Stadium with the corrected finish strip | [track-clean.png](track-clean.png) |
| Flat wordmark | [wordmark.png](wordmark.png) |
| Stella, the roller coordinator | [stella.png](stella.png) |
| Green friend with headphones | [green.png](green.png) |
| Orange friend fleeing the swarm | [orange.png](orange.png) |
| Sleeping yellow friend | [yellow-v2.png](yellow-v2.png) |
| Purple friend stumbling | [purple.png](purple.png) |
| Pink friend riding a bike | [pink-cyclist.png](pink-cyclist.png) |
| White-and-blue sprinter | [white.png](white.png) |
| Bee swarm | [bees.png](bees.png) |
| Perspective checkerboard overlay | [finish-line.png](finish-line.png) |
| Contact and cast shadows | [shadows.png](shadows.png) |
| Assembled banner | [composite.png](composite.png) |

## Rebuild the composite

With Python, Pillow, and NumPy installed, run from the repository root:

```sh
python3 brand/compose.py
```

[compose.py](compose.py) trims transparent margins and scales each source once
for placement. It builds the finish strip as a single projective checkerboard,
adds a separate shadow layer, then alpha-composites the friends and wordmark.
The final image never goes back through generation. The original track is not
overwritten; the corrected background is saved as `track-clean.png`.

The script verifies that no pixels outside the overlay masks changed from the
original background. [manifest.json](manifest.json) records native dimensions,
placement, and SHA-256 hashes. The banner is 2172 × 724 pixels; source cutouts
retain the individual sizes returned by generation rather than being upscaled.

[prompts.txt](prompts.txt) preserves the generation prompts. Update placement or
shadows independently when arranging the scene; do not repeatedly regenerate
the assembled banner. The README uses [the jogging banner](jogging/banner-cute-wave-left.png).
The earlier [group banner](../assets/nova-sprint-banner.png) remains available
as a historical asset.
