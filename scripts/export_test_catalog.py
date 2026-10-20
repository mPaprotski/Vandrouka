#!/usr/bin/env python3
"""Export the committed acceptance snapshot as YAML-compatible JSON for CI only."""
import argparse
import json
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('output', type=Path)
args = parser.parse_args()
root = Path(__file__).resolve().parents[1]
records = json.loads((root / 'backend/testdata/golden_catalog.yaml').read_text())
output = args.output / 'places/minsk'
output.mkdir(parents=True, exist_ok=True)
seen = set()
for record in records:
    slug = record['slug']
    if slug in seen or not slug or any(c not in 'abcdefghijklmnopqrstuvwxyz0123456789-' for c in slug):
        raise ValueError('Invalid or duplicate fixture slug')
    seen.add(slug)
    (output / (slug + '.yaml')).write_text(json.dumps(record, ensure_ascii=False, indent=2) + '\n')
print(f'Exported {len(records)} fixed test records')
