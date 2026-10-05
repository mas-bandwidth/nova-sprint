# Asset provenance

These are the assets shipped with nova-sprint and where they came from.
Keep a provenance entry alongside any new artwork or bundled font. The
original font record moved from Nova Tools on 2026-10-04.

| file | provenance |
|---|---|
| `internal/sprintdash/page/nunito-800.woff2` | **Third-party**: the Nunito typeface, weight 800, latin subset, as Google Fonts serves it (`fonts.gstatic.com/s/nunito/v32/`), copyright 2014 The Nunito Project Authors. It is licensed under the SIL Open Font License 1.1, whose text is `internal/sprintdash/page/OFL.txt` beside it and is embedded in the binary with it. It is the sprint dashboard's wordmark face. |
| `assets/nova-sprint-banner.png` | **Earlier group banner, generated and composited** on 2026-10-05 at Glenn Fiedler's request, developed with Stella. OpenAI image generation rendered the track and transparent friends separately. Ordinary alpha compositing placed the friends and a separate shadow layer over the track, with no final generative edit. A pixel comparison verified that the background stayed unchanged outside the character and shadow masks. Glenn selected this composite with the coordinator's star, green headphones, orange friend's bee swarm, yellow sleeper, purple stumble, pink cartwheel, and white-and-blue runner. PNG, 2172 × 724. |
| `brand/starting-blocks/composite.png` | **Earlier README hero**, composed on 2026-10-05 from a fresh transparent starting-blocks friend, the separate flat wordmark, and a track plate with the grass removed. OpenAI image generation supplied the source renders; deterministic compositing places them without regenerating the assembled image. Original PNGs, prompt, and placement record remain in `brand/starting-blocks/`. PNG, 2170 × 725. |
| `brand/jogging/banner-centered.png` | **Earlier jogging hero**, generated with the built-in OpenAI image tool on 2026-10-05. At Glenn's request, the complete banner was rendered together: a happy jogging, waving mascot and flat wordmark. The final layout centres them as one group. Approved by Glenn; original 2168 × 725 lossless PNG copied unchanged. Final prompt and earlier layout variants are in `brand/jogging/`. |
| `brand/jogging/banner-cute-wave-left.png` | **Current README hero**, approved by Glenn on 2026-10-05. Built-in OpenAI image generation matched the preferred runner's cuter rounded proportions while retaining the jogging-and-waving pose, then refined the spacing between mascot and wordmark. Original 2171 × 724 lossless PNG copied without conversion or resampling. Prompt: `brand/jogging/prompt-cute-wave-left.txt`; source and intermediate variants are preserved alongside it. |
| `brand/yellow-v2.png` | **Corrected sleeping friend**, freshly rendered with the built-in OpenAI image tool on 2026-10-05 at Glenn's request and approved for the explainer. Corrects the malformed lower legs to two knees, two shins, and two feet while retaining the sleepy pose. Transparent 1774 × 887 lossless PNG; original bytes preserved. Prompt: `brand/yellow-v2-prompt.txt`. The original `brand/yellow.png` is retained. |
| `assets/nova-sprint-dashboard.jpg` | **Dashboard preview** captured with the Codex browser on 2026-10-05 from the public demo at `http://69.67.149.151/`. Its rendered markup and displayed values were saved locally, then the LANDED tile was simplified to one line at Glenn's request, matching the accompanying source change. The Cost breakdown and Work tables are folded. The numbers are the captured values, not a fixed performance claim. This previews the requested layout; it does not claim that the live demo has been deployed with that change. |

The artwork extends the Nova Tools workshop and Nova Seed garden's friendly
robot direction. The group banner is preserved without alteration as an
earlier approved composition. See [Brand and voice](BRAND.md) for current usage.

The later [individual render set](../brand/README.md) keeps each friend, the bee
swarm, flat wordmark, and stadium as separate original PNGs, generated with
OpenAI image generation on 2026-10-05 at Glenn's request. Its pink friend rides
a bike. The compositing script creates separate shadow and perspective finish
grid overlays without regenerating the assembled scene. Prompts, placement,
native dimensions, and hashes are saved with those assets. The README now uses
the jogging banner and panels made from this set.

The illustrated [README](../README.md) uses five panels from the six-panel
lossless PNG set in `brand/explainer/`, composed on 2026-10-05 from those
individual assets and the later starting-blocks render. Their labels and workflow
diagrams are drawn by `brand/compose_explainer.py`. Yellow was subsequently
re-rendered with corrected leg anatomy; the other source characters are unchanged. All six panels are drawn at 4× logical resolution and exported as 2× PNGs
with antialiased type, boxes, dividers, checkmarks, and rounded connectors.
Arial is rasterised from the locally installed font, not bundled.
The starting-blocks source, prompt, and composition live in
`brand/starting-blocks/`. Its meaning is a fast friend ready for the next task.

Earlier group banner SHA-256: `86f3ef96309188db3239a0042f7ca7a2915001cde0ff5863109167a203e36291`.

Dashboard screenshot SHA-256: `24531eb43b17cb91eea696a5a08e07ea7b8cdb17fd22b5c46ad541ec29cf8d17`.
