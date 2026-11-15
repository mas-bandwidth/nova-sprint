# Resources in a sprint

How the dealer uses the fleet's resources to decide where a card's work runs.

This page is a design. Nothing on it is built. It was settled with Glenn on
2026-11-15. Rules marked *Rowan's* are choices he left to Rowan and can
overrule.

Resources themselves belong to nova-tools: what a machine has, tags, holds and
CPU units are in nova-tools `docs/RESOURCES.md`, on its `idea/resources`
branch. They work with no sprint. This page is only what a sprint adds: the
dealing, and the view. nova-tools answers questions about resources; the
picture of them is the sprint's board.

## What changes

Today a bench machine is a row with a width: its core count, at most
`fleet_width_cap`, and at most its nova-reserve slots. Every working consumer
takes one of the width, whatever it does.

A machine's width goes away, replaced by its CPU and memory. Glenn: "machine
width in fleet definitely goes away and is replaced by cpu resource."

| Where width is today | What it becomes |
| --- | --- |
| The roster sets a bench's width from its cores, capped | The machine's tags, CPU units and memory, from nova-config |
| The store refuses working consumers above a row's width | The dealer sends a consumer only where its needs are free; on the machine, a hold that finds no room waits |
| The dealer asks whether a row has room in its width | The dealer asks which machine has the card's tags, and its CPU and memory free |
| The board shows working cells out of width | The board shows CPU and memory bars |

`fleet_width_cap` has nothing left to bound.

Tier, routes and a friend's width stay as they are.

Words used here: the **dealer** is the part of the sprint that hands cards to
whoever will work them. The **coordinator** is the AI or person who adds cards
and runs the sprint. A **friend** is an AI that is dealt cards. A **bench** is
a fleet machine that takes sprint work.

## Cards

There are two types of card, and resources touch only one.

A **producer card** is the unit of work in a stream. Producer cards do not
change at all.

A **consumer card** is one go at a producer's work: a work attempt or a read.
It is the consumer that runs, so it is the consumer that holds resources.

A card states what its work needs as one list of names, some with numbers:

    cpu 4, memory 8, mac, metal

The list is a line of the card's brief, as its tier and routes are today
(*Rowan's*). So the producer card's record does not change, and every consumer
cut from it carries the same line.

The coordinator decides the list for each card, by judgement. There is no
table of defaults. Glenn: "it depends on the work. a read is cheaper yes, but
coding might require compiling and tests and so on. you really need to just
use your judgement. in short, 'what resources do you want for this work, as
coordinator?'"

What a name with no number means, and what work holds when it names no `cpu`
or `memory`, is nova-tools' rule and is the same here.

## Who decides where work runs

The dealer decides for the work it places on fleet machines. A friend decides
for the work it is dealt.

Glenn: "dealer decides for what previously was fleet work, friend decides for
work they are dealt."

**The dealer** places a consumer on a machine that has every tag the card
names and reports its CPU and memory free. Higher priority work is placed
first. Nothing running is stopped to make room (*Rowan's*).

The dealer chooses from what machines report, which can be a moment old. The
hold itself is taken on the machine, through nova-reserve, when the work
starts there. If the room has gone by then, the work waits for it, as it waits
for a slot today.

**A friend** takes a card as it does today, up to its own width. It then
chooses where to do the work, and runs each command there through nova-reserve
over ssh, naming what the card needs. Where the friend's session runs does not
matter.

The dealer and a friend can choose the same machine at the same moment. That
is safe: nova-reserve on the machine gives the room to one, and the other
waits.

Glenn: "a friend can run, and ACQUIRE resources to do work, from where it is
running (doesn't matter much), what matters is where it chooses to do the
work."

Friends run today in apps on studio and the macbook. Until a friend can ask
the whole fleet where there is room, its work runs on the machine its app is
on, through nova-reserve there, so it is still counted.

## The board

The section called Fleet becomes Resources. Its rows are still the machines.
Where a row shows its width and working cells today, it shows its CPU, its
memory and its tags.

Glenn: "the current view we have for 'Fleet' turns into 'Resources' and we have
a new view there, resource centric where the width is now, basically CPU,
memory, [tags] like gpu etc."

It answers one question: are we using the fleet's resources fully, yes or no.

- **Bars are split by type of work,** in the colors the board already uses for
  working cells: blocker, critical, fix, read and work. Who is doing the work
  is not shown. CI is a type of work the board has no color for yet.
- **Everything that holds is shown,** CI included.
- **The load column goes,** from Resources and from Friends, which share one
  table. What is held says how busy a machine is. The measurement behind it is
  kept for checking and not drawn.
- **Friends is otherwise unchanged.** It still draws each friend's consumer
  cards against its width. What those cards hold shows in Resources.

The board learns the type of work behind each hold from the hold's label.

## Not settled

1. **A card's reads.** A read is cheaper than the work it reads. Whether a
   card states its reads' needs apart from its work's is not chosen.
2. **The ready, done and ok% columns on a machine's row.** They count the cards
   each machine worked. The board still has a row per machine, but a machine
   is no longer something cards are dealt to, so the counts mean less.
3. **A color for CI** on the board.
4. **Devices: a route or a resource.** A route today already names a device a
   machine serves, and routes stay as they are. A device could also be a
   resource with amount 1. Which one a device is, so there are not two ways to
   say the same thing, is not chosen.
