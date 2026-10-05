# Nova Sprint artwork layers

Fresh individual renders, generated with OpenAI image generation on 2026-10-05
at Glenn's request. Every image here is a **lossless PNG**. The character and
wordmark sources retain their original transparency and resolution. The original
renders are copied byte for byte; there is no JPEG conversion in this pipeline.

[View the solo-runner banner](solo-runner/composite.png), or the earlier
[group composition](composite.png).

## Solo runner

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
with the wordmark on a separate white panel. These are review options; the
README hero remains unchanged.

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
the assembled banner. The README's published asset remains at
[assets/nova-sprint-banner.png](../assets/nova-sprint-banner.png), so a new
composite can be reviewed before replacing it.
