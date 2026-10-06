# Asset provenance

Moved from nova-tools docs/ASSET-PROVENANCE.md on 2026-10-04.

| file | provenance |
|---|---|
| `brand/explainer/coordination-short-legs-dark.png` | **Dark theme variant**, generated with the built-in OpenAI image tool on 2026-10-06 from the approved `coordination-short-legs.png`. Changes the background and label colors while preserving the short-leg character direction. Original lossless PNG copied without resampling. Exact prompt: `brand/explainer/coordination-short-legs-dark-prompt.txt`. |
| `brand/explainer/{machine,streams,team,landed}-dark.png` | **Dark theme diagram compositions**, rendered on 2026-10-06 by `brand/compose_explainer.py --theme dark --panels machine streams team landed`. Reuses the original transparent character assets and layout with a dark palette; 4× drawing, 2× lossless PNG export. Light assets and the README hero remain unchanged. |
| `brand/explainer/coordination-short-legs.png` | Generated with the built-in image generation tool on 2026-10-06 from `brand/explainer/coordination.png`, at Glenn's request to shorten Yellow's legs. The original remains available. The exact edit prompt is in `brand/explainer/coordination-short-legs-prompt.txt`. |
| `internal/sprintdash/page/nunito-800.woff2` | **Third-party**: the Nunito typeface, weight 800, latin subset, as Google Fonts serves it (`fonts.gstatic.com/s/nunito/v32/`), copyright 2014 The Nunito Project Authors. It is licensed under the SIL Open Font License 1.1, whose text is `internal/sprintdash/page/OFL.txt` beside it and is embedded in the binary with it. It is the sprint dashboard's wordmark face. |
