#!/usr/bin/env python3
"""Generate the focused Clear refinement and exact negative/limitation cases."""
from pathlib import Path

HERE = Path(__file__).resolve().parent
INV = ["TypeOK", "CursorHasCoverage", "NoUncommittedSend", "CancellationHasClear"]
PROPS = ["AllAccounted", "AllCursorsAdvance"]
CASES = [
    ("RunClearSummaryLost", "CursorHasCoverage", "invariant", "FAULT",
     "Suppress old notices but omit the Clear summary, then pass its cursor.",
     [("needed' = IF old THEN needed ELSE needed \\cup {n}",
       "needed' = IF old \\/ (log[n].kind = \"clear\" /\\ cancelled # {}) THEN needed ELSE needed \\cup {n}")]),
    ("RunClearCursorEarly", "CursorHasCoverage", "invariant", "FAULT",
     "Save the cursor before all required notice and summary sends confirm.",
     [("SaveCursor ==\n  /\\ phase = \"send\" /\\ needed \\subseteq acknowledged",
       "SaveCursor ==\n  /\\ phase = \"send\"")]),
    ("RunClearInFlight", "NoOldSendAfterClear", "invariant", "ACCEPTED LIMITATION",
     "A prepared old-epoch message may send before this pusher observes Clear.", []),
    ("RunClearCountAmbiguous", "ClearCountIsUndelivered", "invariant", "ACCEPTED LIMITATION",
     "Confirmed-but-cursor-pending notices are included in the summary count.", []),
    ("RunClearOriginalCancelled", "EveryOriginalSent", "temporal", "ACCEPTED LIMITATION",
     "A Clear summary replaces original old-epoch delivery obligations.", []),
]

def cfg():
    return "CONSTANTS MaxNotices = 2\n          MaxCrashes = 1\nSPECIFICATION Spec\nCHECK_DEADLOCK FALSE\n"

def main():
    base = (HERE / "RunClear.tla").read_text()
    (HERE / "RunClear.cfg").write_text(cfg() + "".join("INVARIANT " + x + "\n" for x in INV)
                                        + "".join("PROPERTY " + x + "\n" for x in PROPS))
    directory = HERE / "witnesses" / "layer6"
    for name, target, kind, label, explanation, replacements in CASES:
        text = base.replace("MODULE RunClear ", "MODULE " + name + " ", 1)
        first, rest = text.split("\n", 1)
        text = first + "\n\\* " + label + ": " + explanation + "\n\\* Must violate " + target + ".\n" + rest
        for old, new in replacements:
            assert text.count(old) == 1, (name, old, text.count(old))
            text = text.replace(old, new)
        (directory / (name + ".tla")).write_text(text)
        (directory / (name + ".cfg")).write_text("\\* " + label + ": expected " + target + " violation.\n"
            + cfg() + ("INVARIANT " if kind == "invariant" else "PROPERTY ") + target + "\n")

if __name__ == "__main__":
    main()
