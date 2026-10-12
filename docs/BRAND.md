# Nova Sprint: finish faster

nova-sprint is the running track in the Nova Tools family: a cute, capable
runner and a practical system for moving work from ready to landed.

The product promise is **multiple models, many machines**. Workers run bounded
assignments across a fleet. Width lets independent work happen at the same
time; the system carries cards through implementation, review, checks,
merging, and landing within the configured rules.

nova-sprint supplements existing ways of working with AIs. Keep the familiar
workflow for conversation, exploration, and direction. Use the fleet when
there is a batch of work to hand off.

## Name and language

Call nova-sprint the **system**. Reserve **machine** and **machines** for
fleet computers. The system coordinates; machines supply capacity. Preserve
literal command syntax, output, and historical quotations. The technical
term **state machine** still describes a formal model.

Write **nova-sprint** in prose and command names. The artwork uses **Nova Sprint**.
Link to [Nova Tools](https://github.com/mas-bandwidth/nova-tools) when explaining
the product family and foundation.

Use **workers**, **fleet**, **lanes**, **width**, **cards**, and **work streams**.
A worker carries out an assignment. The fleet supplies machines. Width is
concurrent capacity; a lane is its visual metaphor. Explain those terms
before relying on them. Multiple models and many machines are both part of
the promise.

## Approved artwork

The [daytime banner](../brand/fleet/banner-day.png) leads the README.
The [nighttime banner](../brand/fleet/banner-night.png) is selected through
GitHub's `prefers-color-scheme: dark` picture source. Daytime is the fallback.
Both carry the same text:

> Finish faster.
> Multiple models. Many machines.

Keep the rounded wordmark above the headline, a blue track with horizontal
lane lines, and the white-and-blue robot in orange shoes. The face contains
only two cyan diagonal eye strokes. The rear foot pushes off in the same
lane as the starting blocks. The nighttime scene has no robot shadow.

The [square badge](../brand/fleet/nova-sprint-square.png) uses playful stacked
lettering, three gold beams, and a rich navy rounded tile extended to the
image edges. Preserve the approved letter shapes and internal spacing.

Do not introduce additional slogans. Keep track, robot, and shadow as
separate layers for future compositions so placement stays intentional.
Record new assets in [Asset provenance](ASSET-PROVENANCE.md).

## Show the product working

Explain the batch of work, how width helps, and how the system keeps cards
moving. Reuse clear explanations of cards, streams, dependencies, gates,
review, and landing. Gates that need a decision remain gates; speed does
not mean skipping checks.

Screenshots are snapshots. Preserve actual values and record where and when
they were captured. Keep command names and technical contracts accurate,
even where the branding vocabulary is simpler than an implementation name.

## Colours and type

Use navy, bright blue, white, and gold for the mark. Orange shoes add warmth.
The nighttime illustration uses moonlight and warm campus lamps. The square
badge's background is `#081d46`.

The dashboard's functional palette retains its existing state meanings:

| Role | Dark theme | Light theme |
|---|---|---|
| Background | `#0d0f12` | `#f4f5f7` |
| Surface | `#14171b` | `#ffffff` |
| Main text | `#eef0f3` | `#121417` |
| Working | `#3987e5` | `#2a78d6` |
| Review | `#d55181` | `#e87ba4` |
| Merging | `#c98500` | `#eda100` |
| Landed | `#199e70` | `#1baf7a` |

The [dashboard contract](SPEC-SPRINT-DASHBOARD.md) owns UI layout and state
colours. Branding artwork does not redefine them.

The lettering is part of the approved artwork. Use ordinary Markdown
headings, short paragraphs, and purposeful tables in documentation.
The dashboard's bundled Nunito font carries the SIL Open Font License.

## Voice

Warm, capable, and lightly playful. Explain the useful thing first; let the
robot and an occasional joke supply the charm. The little orange shoes are
welcome. A sporting metaphor in every sentence is a bit much cardio.

| Prefer | Avoid |
|---|---|
| “Give the fleet the next batch.” | Making every handoff an instruction for the human. |
| “A card is one task, with a clear finish condition.” | Internal state vocabulary before defining it. |
| “More lanes let independent work run at the same time.” | Promising that width removes dependencies. |
| “This card is waiting for review.” | “The system is stuck” without explaining what needs attention. |
| “The change landed; installation is still pending.” | “All done” when the evidence only shows a pushed branch. |

Preserve historical reports, quotations, and earlier artwork as evidence.
The previous branding remains on the archive branch linked from
[the artwork notes](../brand/fleet/README.md).
