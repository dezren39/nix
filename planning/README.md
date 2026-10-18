# Planning

This folder holds the plans for this macOS configuration repo at three levels of detail.

| Level   | Question it answers                     | Where                                                                                          | How it changes                                  |
| ------- | --------------------------------------- | ---------------------------------------------------------------------------------------------- | ----------------------------------------------- |
| Roadmap | Where is this going, and in what order? | [`ROADMAP.md`](../ROADMAP.md)                                                                  | Edited in place, kept short                     |
| Design  | What will it be, and why this way?      | [`designs/`](designs/), plus [`ARCHITECTURE.md`](../ARCHITECTURE.md) for how things work today | Dated records, appended to while work is active |
| Plan    | How do we build it, one step at a time? | [`plans/`](plans/)                                                                             | Updated as each step lands                      |

The roadmap is the top level. An item can sit there as one line with no details, long before anyone knows how to build it. A design is written when the shape of the thing has to be decided. A plan is written when the work is ready to start.

## Roadmap

This repo is one nix-darwin configuration plus the packages and patches around it, so it has one roadmap file, [`ROADMAP.md`](../ROADMAP.md) at the repo root. It has a section for each area.

Each roadmap row has a horizon.

| Horizon | Meaning                                                                       |
| ------- | ----------------------------------------------------------------------------- |
| Now     | Being worked on, or first in line. Has an issue.                              |
| Next    | The direction is chosen and the shape is mostly known. Starts after Now.      |
| Later   | The direction is chosen, the shape is not. Details are allowed to be missing. |
| Ideas   | Written down so it is not lost. No commitment, and it may be dropped.         |

Each item has a status.

| Status        | Meaning                                                               |
| ------------- | --------------------------------------------------------------------- |
| `idea`        | Someone wrote it down. Details may be missing.                        |
| `proposed`    | Described with a reason. Waits for a decision to go ahead.            |
| `shaped`      | The direction is decided. A design record exists or is being written. |
| `planned`     | A plan with steps exists and the issues are filed.                    |
| `in-progress` | At least one PR for it is open or merged.                             |
| `shipped`     | Done. The row moves to the "Done" table with the date and the PR.     |
| `dropped`     | Decided against. The row moves to the "Done" table with the reason.   |

A roadmap row records intent. It is not a go-ahead to build the thing, and it does not settle a product question. Wherever this repo says a maintainer decides, a row at `proposed` or earlier still waits for that decision.

## How to use it

1. Add a new idea as one row under Ideas. One sentence of intent is enough.
2. Change a row's horizon or status in the same PR as the work that causes the change.
3. Link every row to its issues. When a design or plan exists, link that too.
4. When a row ships or is dropped, move it to the "Done" table. Do not delete rows, since the history is useful.
5. Open an issue for every item in Now, so the work can be picked up and tracked there. The roadmap links to issues and does not copy their content.

## Templates

- [Roadmap item](templates/roadmap-item.md)
- [Design record](templates/design.md)
- [Implementation plan](templates/plan.md)

## Where existing material lives

- [`ARCHITECTURE.md`](../ARCHITECTURE.md) describes the repo as it is. A design record in [`designs/`](designs/) describes intent while the work is open. When the work ships, update `ARCHITECTURE.md`.
- [`docs/`](../docs/) holds the sidepulse notes.
- [`TODO.md`](../TODO.md) is a scratch pad of old configuration notes. Its useful items are listed in the roadmap's Ideas rows, and it is not a plan.
- [`.agents/skills/`](../.agents/skills/) holds the repo-local agent skills, including `update-opencode` and `nix-config`.

## Related repositories

| Repository                     | Relation                                                                               |
| ------------------------------ | -------------------------------------------------------------------------------------- |
| `developing-today/code`        | Primary source for `mcpx`. This repo vendors a copy today and is moving to consume it. |
| `developing-today/opencode`    | OpenCode fork, built here and pinned in `flake.nix`.                                   |
| `developing-today/openchamber` | OpenChamber fork, built against that OpenCode.                                         |
