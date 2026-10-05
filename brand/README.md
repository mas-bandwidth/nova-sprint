# Nova Sprint artwork layers

Fresh individual renders, generated with OpenAI image generation on 2026-10-05
at Glenn's request. Every image here is a **lossless PNG**. The character and
wordmark sources retain their original transparency and resolution. The original
renders are copied byte for byte; there is no JPEG conversion in this pipeline.

[View the starting-blocks banner](starting-blocks/composite.png), or the earlier
[group composition](composite.png).

## What the friends represent

These are Glenn's character meanings for Nova Sprint. Keep them consistent
when using the individual assets in illustrations and documentation.

| Character | Meaning |
|---|---|
| [Little robot with the checklist](stella.png) | The coordinator. |
| [Sleeping yellow friend](yellow.png) | Friends falling asleep when they should be working together. |
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
No character was regenerated for the explainer.

## Starting blocks

The latest direction returns to the original mascot, freshly rendered on the
starting blocks at the left, with the flat wordmark ahead of him. The grass
strip is removed by continuing the pale background to the track edge; lane
pixels are preserved. All source and placed layers are lossless PNGs in
[starting-blocks](starting-blocks/), with the generation
[prompt](starting-blocks/prompt.txt) and placement record alongside them.

Rebuild with `python3 brand/compose_starting_blocks.py` (Pillow and NumPy).
The script also saves the previous [running version without grass](starting-blocks/running-no-grass.png).
This is the selected README hero.

## Solo runner

The revised [side-on version](solo-runner-v2/composite.png) uses a freshly
generated quiet track plate, with left-to-right lanes and open sky behind the
wordmark. The original character and logo are composited without regeneration.
Its lossless layers and [background prompt](solo-runner-v2/prompt.txt) are saved
in `solo-runner-v2/`. Rebuild with `python3 brand/compose_solo_v2.py` (Pillow
required). The script checks that background pixels outside the overlays remain
unchanged. These earlier options are retained alongside the selected starting-blocks hero.

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
| Sleeping yellow friend | [yellow.png](yellow.png) |
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
the assembled banner. The README uses [the starting-blocks banner](starting-blocks/composite.png).
The earlier [group banner](../assets/nova-sprint-banner.png) remains available
as a historical asset.
