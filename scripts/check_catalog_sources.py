#!/usr/bin/env python3
"""Check the committed GPX subset against its provenance manifest, offline."""
import json
from pathlib import Path
import xml.etree.ElementTree as ET


def main():
    root = Path(__file__).resolve().parents[1]
    manifest = json.loads((root / 'data/sources/globus-minsk.json').read_text())
    ns = {'g': 'http://www.topografix.com/GPX/1/1'}
    gpx = ET.parse(root / 'data/sources/globus-minsk.gpx').getroot()
    selected = [p for p in manifest['waypoints'] if p['action'] != 'excluded']
    points = gpx.findall('g:wpt', ns)
    if len(points) != len(selected) or len(points) != manifest['selected_count']:
        raise ValueError('GPX selection count differs from manifest')
    if len({p['index'] for p in manifest['waypoints']}) != len(manifest['waypoints']):
        raise ValueError('Duplicate source indices')
    if len({p['slug'] for p in selected}) != len(selected):
        raise ValueError('Duplicate mapped places')
    for key, action in [('new_count', 'added'), ('existing_count', 'matched')]:
        if manifest[key] != sum(p['action'] == action for p in selected):
            raise ValueError(f'Incorrect {key}')
    if gpx.findtext('g:metadata/g:time', namespaces=ns) != manifest['source_date']:
        raise ValueError('Source dates differ')
    for source, point in zip(selected, points):
        for field in ['lat', 'lon']:
            if float(point.attrib[field]) != source[field]:
                raise ValueError(f"Coordinates differ for {source['slug']}")
        for xml_field, json_field in [('name', 'original_name'), ('desc', 'description'), ('time', 'waypoint_time')]:
            if point.findtext('g:' + xml_field, namespaces=ns) != source[json_field]:
                raise ValueError(f"Source {xml_field} differs for {source['slug']}")
        if not (root / 'data/places/minsk' / (source['slug'] + '.yaml')).is_file():
            raise ValueError(f"Missing catalog record: {source['slug']}")
    print(f'Checked {len(points)} GPX objects and their catalog mappings')


if __name__ == '__main__':
    main()
