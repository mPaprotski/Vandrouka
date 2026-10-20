#!/usr/bin/env python3
"""Import the reviewed Minsk selection from the user-supplied Globus GPX.

No network or database access. Indices are zero-based waypoint positions, not
GPX IDs. Explicit matching avoids merging unrelated objects by name/proximity.
"""
import argparse
import os
import hashlib
import json
from pathlib import Path
import xml.etree.ElementTree as ET

NS = {'g': 'http://www.topografix.com/GPX/1/1'}
SOURCE_URL = 'https://globus.by/'
SOURCE_SHA256 = '4ebb7dc1c007f97a05829efe1a004dd4caa16c61006bcc89962ff10c03393da9'
# index: slug, display name, categories, editorial minutes
NEW = {
300: ('upper-town', 'Верхний город', ['streets', 'architecture'], 30),
301: ('guest-court', 'Гостиный двор', ['architecture'], 15),
302: ('masons-house', 'Дом масонов — внешний осмотр', ['architecture'], 15),
303: ('calvary-church', 'Костёл Воздвижения Святого Креста на Кальварии', ['religious', 'architecture'], 20),
307: ('saint-joseph-church', 'Бывший костёл Святого Иосифа', ['religious', 'architecture'], 20),
309: ('basilian-monastery-building', 'Жилой корпус бывшего базилианского монастыря Святого Духа', ['architecture', 'religious'], 15),
310: ('basilian-convent-building', 'Жилой корпус бывшего базилианского женского монастыря', ['architecture', 'religious'], 15),
311: ('trinity-basilian-convent', 'Жилой корпус бывшего Троицкого монастыря базилианок', ['architecture', 'religious'], 15),
312: ('boulder-museum', 'Музей валунов', ['parks', 'museums'], 40),
313: ('pishchalovsky-castle', 'Пищаловский замок — внешний осмотр', ['architecture'], 20),
316: ('white-dacha', 'Усадьба «Белая дача»', ['architecture'], 25),
317: ('vankovich-large-slepyanka', 'Усадьба Ваньковичей «Большая Слепянка»', ['architecture'], 25),
319: ('freedom-square-manor', 'Городская усадьба на площади Свободы, 15', ['architecture'], 15),
320: ('alexander-nevsky-church', 'Церковь Святого Александра Невского', ['religious', 'architecture'], 20),
321: ('mary-magdalene-church', 'Церковь Святой Марии Магдалины', ['religious', 'architecture'], 20),
323: ('calvary-gate', 'Брама Кальварийского кладбища', ['architecture'], 15),
1526: ('loshitsa-manor-house', 'Лошицкая усадьба — усадебный дом', ['architecture'], 20),
1527: ('loshitsa-chapel', 'Лошицкая усадьба — руины часовни Прушинских', ['religious', 'architecture'], 15),
1528: ('loshitsa-distillery', 'Лошицкая усадьба — винокурня', ['architecture'], 15),
1529: ('loshitsa-workers-house', 'Лошицкая усадьба — дом рабочих винокурни', ['architecture'], 10),
1530: ('loshitsa-mill-ruins', 'Лошицкая усадьба — руины водяной мельницы', ['architecture'], 15),
1531: ('loshitsa-water-mill', 'Лошицкая усадьба — здание водяной мельницы', ['architecture'], 15),
1532: ('loshitsa-manager-house', 'Лошицкая усадьба — дом управляющего', ['architecture'], 15),
1533: ('loshitsa-service-building', 'Лошицкая усадьба — служебный корпус', ['architecture'], 10),
1534: ('loshitsa-gate', 'Лошицкая усадьба — брама и сторожка', ['architecture'], 15),
}
EXISTING = {304: 'red-church', 305: 'roch-church', 306: 'holy-spirit-cathedral',
308: 'mary-cathedral', 314: 'town-hall', 315: 'trinity-suburb',
318: 'vankovich-house', 322: 'peter-paul-cathedral'}
EXCLUDED = {362: 'Вне Минска: Сёмково', 366: 'Вне Минска: Сёмково',
367: 'Вне Минска: Сеница', 651: 'Вне Минска: Новый Двор',
754: 'Вне Минска: музей в Озерце', 951: 'Вне Минска: Колодищи',
1264: 'Вне Минска: Куропаты, Минский район',
**{i: 'Вне Минска: Прилуки' for i in range(1456, 1463)}}


def yaml_record(record):
    # JSON is also valid YAML; emit familiar YAML keys and JSON-escaped values.
    lines = []
    for key, value in record.items():
        if isinstance(value, list) and value:
            lines.append(key + ':')
            for item in value:
                if isinstance(item, dict):
                    for i, (k, v) in enumerate(item.items()):
                        lines.append(('  - ' if i == 0 else '    ') + k + ': ' + json.dumps(v, ensure_ascii=False))
                else:
                    lines.append('  - ' + json.dumps(item, ensure_ascii=False))
        else:
            lines.append(key + ': ' + json.dumps(value, ensure_ascii=False))
    return '\n'.join(lines) + '\n'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('gpx', type=Path)
    parser.add_argument('--date', default='2026-10-13', help='Date of source-file review, not field verification')
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    data_root = Path(os.environ.get('DATA_DIR', str(root.parent / 'data')))
    raw = args.gpx.read_bytes()
    if hashlib.sha256(raw).hexdigest() != SOURCE_SHA256:
        raise SystemExit('GPX checksum differs; review the selection before importing another file')
    tree = ET.fromstring(raw)
    points = tree.findall('g:wpt', NS)
    if len(points) != 1583 or tree.findtext('g:metadata/g:time', namespaces=NS) != '2015-12-28T17:03:05Z':
        raise SystemExit('Unexpected GPX version; review waypoint mapping before importing')
    selected = set(NEW) | set(EXISTING)
    nearby = {i for i, w in enumerate(points) if 53.75 <= float(w.attrib['lat']) <= 54.05 and 27.3 <= float(w.attrib['lon']) <= 27.85}
    if nearby != selected | set(EXCLUDED):
        raise SystemExit('Selection does not account for all nearby waypoints; review required')
    report = dict(source=args.gpx.name, sha256=hashlib.sha256(raw).hexdigest(),
                  source_date='2015-12-28T17:03:05Z', reviewed_at=args.date,
                  total_waypoints=len(points), selected_count=len(selected), new_count=len(NEW),
                  existing_count=len(EXISTING), waypoints=[])
    subset = ET.Element('{'+NS['g']+'}gpx', version='1.1', creator='Vandrouka: reviewed Globus Minsk subset')
    metadata = ET.SubElement(subset, '{'+NS['g']+'}metadata')
    ET.SubElement(metadata, '{'+NS['g']+'}time').text = report['source_date']
    for i in sorted(nearby):
        w = points[i]
        original_name = w.findtext('g:name', namespaces=NS)
        lat, lon = float(w.attrib['lat']), float(w.attrib['lon'])
        rec = dict(index=i, original_name=original_name, lat=lat, lon=lon,
                   description=w.findtext('g:desc', namespaces=NS),
                   waypoint_time=w.findtext('g:time', namespaces=NS))
        if i in EXCLUDED:
            rec.update(action='excluded', reason=EXCLUDED[i])
        else:
            subset.append(w)
            slug = EXISTING.get(i) or NEW[i][0]
            path = data_root / 'places/minsk' / (slug+'.yaml')
            title = f'Globus: GPX 2015-12-28, точка {i}; см. data/sources/globus-minsk.json'
            link = dict(kind='article', url=SOURCE_URL, title=title)
            if i in EXISTING:
                if not path.exists():
                    raise SystemExit(f'Missing existing place: {slug}')
                content = path.read_text()
                if SOURCE_URL not in content:
                    # Existing curated YAML stays intact; add provenance only.
                    content += '- kind: article\n  url: '+SOURCE_URL+'\n  title: '+json.dumps(title, ensure_ascii=False)+'\n'
                    path.write_text(content)
                rec.update(action='matched', slug=slug)
            else:
                _, name, cats, minutes = NEW[i]
                summary = (f'{name}. Объект отмечен в предоставленной выгрузке Globus от 28 декабря 2015 года. '
                           'Остановка предполагает внешний осмотр с доступного общественного подхода; '
                           'вход внутрь и на закрытую территорию не включён. '
                           'Состояние объекта, доступность подхода и условия посещения требуют актуальной проверки.')
                place = dict(slug=slug, name=name, summary=summary, lat=lat, lon=lon,
                             importance=3, must_see=False, visit_minutes=minutes,
                             hours_known=False, price_known=False, verified_at=args.date,
                             categories=cats, opening_hours={}, opening_exceptions=[], prices=[], links=[link])
                if path.exists():
                    # Re-run must not overwrite later editorial verification.
                    if title not in path.read_text():
                        raise SystemExit(f'Slug collision: {slug}')
                else:
                    path.write_text(yaml_record(place))
                rec.update(action='added', slug=slug)
        report['waypoints'].append(rec)
    (data_root/'sources/globus-minsk.json').write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    ET.register_namespace('', NS['g'])
    ET.indent(subset)
    ET.ElementTree(subset).write(data_root/'sources/globus-minsk.gpx', encoding='utf-8', xml_declaration=True)
    print(f'{len(selected)} Minsk objects: {len(NEW)} new, {len(EXISTING)} matched; {len(EXCLUDED)} nearby objects excluded')


if __name__ == '__main__':
    main()
