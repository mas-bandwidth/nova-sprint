# Resources in a sprint

How the dealer uses the fleet's resources to decide where a card's work runs.

This page is a design. Nothing on it is built. It was settled with Glenn on
2026-11-15. Rules marked *Rowan's* are choices he left to Rowan and can
overrule.

Resources themselves belong to nova-tools: what a machine has, tags, holds,
CPU units and the resource view are in nova-tools `docs/RESOURCES.md`, on its
`idea/resources` branch. They work with no sprint. This page is only what a
sprint adds.

## What changes

Today a bench machine is a row with a width: its core count, at most
`fleet_width_cap`, and at most its nova-reserve slots. Every working consumer
takes one of the width, whatever it does.

A machine's width goes away, replaced by its CPU and memory. Glenn: "machine
width in fleet definitely goes away and is replaced by cpu resource."

| Where width is today | What it becomes |
| --- | --- |
| The roster sets a bench's width from its cores, capped | The machine's tags, CPU units and memory, from nova-config |
| The store refuses working consumers above a row's width | Work is refused a hold that would exceed the machine's CPU or memory |
| The dealer asks whether a row has room in its width | The dealer asks which machine has the card's tags, and its CPU and memory free |
| The board shows working cells out of width | The board shows CPU and memory bars |

`fleet_width_cap` has nothing left to bound.

Tier, routes and a friend's width stay as they are.

## Cards

There are two types of card, and resources touch only one.

A **producer card** is the unit of work in a stream. Producer cards do not
change at all.

A **consumer card** is one go at a producer's work: a work attempt or a read.
It is the consumer that runs, so it is the consumer that holds resources.

A card states what its work needs as one list of tags and numbers, and a
number left out means 1:

    cpu 4, memory 8, mac, metal

The coordinator decides the list for each card, by judgement. There is no
table of defaults. Glenn: "it depends on the work. a read is cheaper yes, but
coding might require compiling and tests and so on. you really need to just
use your judgement. in short, 'what resources do you want for this work, as
coordinator?'"

A card that names no `cpu` or `memory` holds 1 of each (*Rowan's*).

## Who decides where work runs

The dealer decides for the work it places on fleet machines. A friend decides
for the work it is dealt.

Glenn: "dealer decides for what previously was fleet work, friend decides for
work they are dealt."

**The dealer** places a consumer on a machine that has every tag the card
names and has its CPU and memory free. Higher priority work is placed first.
Nothing running is stopped to make room (*Rowan's*).

**A friend** takes a card as it does today, up to its own width. It then
chooses where to do the work: it asks for the resources the card names, gets a
hold on a machine that has them, and works there over ssh. Where the friend's
session runs does not matter.

Glenn: "a friend can run, and ACQUIRE resources to do work, from where it is
running (doesn't matter much), what matters is where it chooses to do the
work."

Friends run today in apps on studio and the macbook. Until a friend can ask
the whole fleet for a hold, its work runs on the machine its app is on.

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
- **The load column goes.** What is held says how busy a machine is. The
  measurement behind it is kept for checking and not drawn.
- **Friends is unchanged.** It still draws each friend's consumer cards
  against its width. What those cards hold shows in Resources.

## Not settled

1. **A card's reads.** A read is cheaper than the work it reads. Whether a
   card states its reads' needs apart from its work's is not chosen.
2. **The ready, done and ok% columns on a machine's row.** They count cards per
   machine, which means less once a machine is not a row that takes cards.
3. **A color for CI** on the board.
