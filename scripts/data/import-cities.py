#!/usr/bin/env python3
"""Convert a reviewed hflabs/city CSV snapshot into our shared city directory."""
import argparse
import collections
import csv
import json
import pathlib

parser = argparse.ArgumentParser()
parser.add_argument('csv', type=pathlib.Path)
parser.add_argument('--commit', required=True)
args = parser.parse_args()
rows = list(csv.DictReader(args.csv.open(encoding='utf-8-sig')))
assert all(row['address'].split(', ')[-1].startswith('г ') for row in rows)
names = [row['address'].split(', ')[-1][2:] for row in rows]
counts = collections.Counter(name.lower().replace('ё', 'е') for name in names)
aliases = {'Москва': ['мск'], 'Санкт-Петербург': ['спб', 'питер', 'санкт петербург'], 'Екатеринбург': ['екб']}
cities = []
for row, name in zip(rows, names):
    region = row['region'] if row['region_type'] == 'г' else f"{row['region']} {row['region_type']}"
    value = f'{name} ({region})' if counts[name.lower().replace('ё', 'е')] > 1 else name
    cities.append({'id': row['fias_id'], 'name': name, 'region': region, 'value': value,
                   'population': int(row['population'] or 0), 'aliases': aliases.get(name, [])})
cities.sort(key=lambda city: (-city['population'], city['value']))
assert len({city['id'] for city in cities}) == len(cities)
assert len({city['value'] for city in cities}) == len(cities)
result = {'source': f'https://github.com/hflabs/city/tree/{args.commit}', 'license': 'CC-BY-SA-4.0', 'cities': cities}
output = pathlib.Path(__file__).resolve().parents[2] / 'backend/internal/geography/cities.json'
output.write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')
print(f'Imported {len(cities)} cities to {output}')
