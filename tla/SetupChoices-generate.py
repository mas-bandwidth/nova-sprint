#!/usr/bin/env python3
"""Generate the two explicit SetupChoices reversed-witness modules."""
from pathlib import Path

HERE = Path(__file__).resolve().parent
source = (HERE / 'SetupChoices.tla').read_text()
replacements = {
    'SetupChoicesNoneNeedsKey': (
        'IF servicesChoice = "none" THEN {} ELSE KeyQuestions', 'KeyQuestions'),
    'SetupChoicesReprompt': (
        '/\\ phase = "repair" /\\ q \\in Need(pc) /\\ q \\notin answers',
        '/\\ phase = "repair" /\\ q \\in Need(pc)'),
}
for name, (before, after) in replacements.items():
    assert before in source
    witness = source.replace('MODULE SetupChoices ', 'MODULE ' + name + ' ', 1)
    # Reprompt deliberately changes Ask only, leaving Supply guarded.
    witness = witness.replace(before, after, 1)
    (HERE / (name + '.tla')).write_text(witness)
