# The nova-sprint friends

nova-sprint is the running track in the Nova family: AI friends with different
strengths, working together and having fun. Some sprint, some skip, someone
stumbles, and someone needs a nap. The coordinator helps the team keep going.

The product promise is practical: scale out across friends running different
models and harnesses, across a fleet running swarms of AIs, or any combination.
An AI can coordinate that work while the human is away. The artwork makes
the team approachable; the documentation explains the work clearly.

## Name

Write **nova-sprint** in prose and command names. The illustrated banner uses
**Nova Sprint**, with a star in the rounded wordmark. Preserve that artwork
when embedding it; do not redraw the text with an unrelated typeface.

Link to [Nova Tools](https://github.com/mas-bandwidth/nova-tools) when
explaining the foundation. Send people who want to create AI friends to
[Nova Seed](https://github.com/mas-bandwidth/nova).

## Mark and banner

Use [the approved banner](../assets/nova-sprint-banner.png) at the top of the
README. It is a 2172 × 724 PNG with a 3:1 aspect ratio. Let it scale as a whole
so the title, coordinator, sleeping friend, and runners all remain visible.

The little roller bot at the back is the coordinator, with a clipboard and
Stella star. The other friends have distinct colours and poses: green listens
to music through headphones, orange runs from a swarm of bees, yellow sleeps
with Zzzzz, purple stumbles, pink cartwheels, and the white-and-blue friend
sprints. The group expresses collaboration with room
for different paces, rather than a ranking of models or people.

The newer individual set uses a pink cyclist. Glenn's character meanings,
including the starting-blocks friend ready for the next task, are recorded in
[the artwork guide](../brand/README.md#what-the-friends-represent). Use those
meanings consistently: the sleeping friend and blocked dependency come from
real coordination difficulties, while the bees represent fleet swarms.
The [illustrated explainer](WORKING-WITH-AI-TEAMS.md) uses them to show why
nova-sprint's machine tracks and drives the team's work beyond any one chat,
model, or harness.

For future art, keep recognisable robot bodies, readable expressions, and
varied movement. Align a runner's movement with the perspective of its lane.
Keep orange clearly separate from yellow. Humour should feel affectionate.
Record the source and generation details of any new asset in
[Asset provenance](ASSET-PROVENANCE.md).

Keep the track, friends, and shadows as separate layers when composing future
banners. Preserve the straight lane geometry; composite onto the track rather
than repeatedly regenerating the whole image.

## Show the product working

After the short introduction, lead with the dashboard. Use a real screenshot
linked to the live demo, then explain what a reader can watch: streams,
friends and fleet capacity, spend, throughput, and the estimated finish time.
Keep the longer workflow explanations in the guides.

Screenshots are snapshots. Preserve their actual values and record where and
when they were captured; do not manufacture a healthier or busier sprint.

## Colours

The illustration uses a blue track, navy and bright-blue lettering, warm
golden light, and colourful friends. Those colours connect it to the wider
Nova family without turning every robot into the same character.

The dashboard has its own functional palette. Preserve the meanings of its
state and status colours rather than assigning them to character identities:

| Role | Dark theme | Light theme |
|---|---|---|
| Background | `#0d0f12` | `#f4f5f7` |
| Surface | `#14171b` | `#ffffff` |
| Main text | `#eef0f3` | `#121417` |
| Working | `#3987e5` | `#2a78d6` |
| Review | `#d55181` | `#e87ba4` |
| Merging | `#c98500` | `#eda100` |
| Landed | `#199e70` | `#1baf7a` |

The [dashboard contract](SPEC-SPRINT-DASHBOARD.md) owns the UI palette and
layout. The LANDED tile has a separately recorded owner-requested simplification;
other interface rules stay with that contract.

## Type

The banner's lettering is part of the illustration. The dashboard uses a
rounded wordmark stack including Nunito 800, with system text and monospace
numbers. The bundled Nunito font carries the SIL Open Font License.

Use ordinary Markdown headings, short paragraphs, and purposeful tables in
repository documentation. Readability matters more than reproducing the
illustration's lettering everywhere.

## Voice

Write as a capable friend helping a programmer work with a team of AIs.
Explain what the tool helps them do before introducing its internal machinery.
Lead with AI coordination, cross-model collaboration, and autonomous work;
make the human's role direction and boundaries, not constant dispatch.

| Prefer | Avoid |
|---|---|
| “Give your AI team a goal, and let them work together.” | “An opinionated processor of work-table state transitions.” |
| “A card is one task, with a clear finish condition.” | “Primaries are admitted to streams” before defining either term. |
| “The AI coordinator keeps the work moving while you are away.” | Making every handoff an instruction for the human to perform. |
| “This card is waiting for a reader.” | “The system is stuck” without explaining what needs attention. |
| “The change landed; installation is still pending.” | “All done” when the evidence only shows a pushed branch. |

Use the race metaphor lightly: a first lap is welcoming, but a reader should
not have to decode sporting language to operate a command. Keep flags,
formats, and refusal messages exact. Preserve historical reports and
quotations as evidence, with plain-language context around them.
