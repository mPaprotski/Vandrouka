#!/usr/bin/env python3
"""Verify checksums and restore PostgreSQL into a unique disposable database only.
MinIO files restore into a temporary directory, never the running volume.
"""
import argparse,json,pathlib,subprocess,tempfile,tarfile,uuid
from backup_local import COMPOSE,run,output,digest,row_counts
p=argparse.ArgumentParser(description=__doc__);p.add_argument('directory');args=p.parse_args();source=pathlib.Path(args.directory).resolve()
m=json.loads((source/'manifest.json').read_text())
if m.get('version') not in (1,2):raise ValueError('Unsupported manifest version')
for name in ['postgres.dump','minio.tar.gz']:
 if digest(source/name)!=m['files'][name]:raise ValueError('Checksum mismatch')
db='restore_check_'+uuid.uuid4().hex
try:
 run(COMPOSE+['exec','-T','postgres','createdb','-U','postgres',db],stdout=subprocess.DEVNULL)
 with (source/'postgres.dump').open('rb') as f:run(COMPOSE+['exec','-T','postgres','pg_restore','-U','postgres','-d',db,'--no-owner','--no-privileges','--exit-on-error'],stdin=f,stdout=subprocess.DEVNULL)
 if m['version']==2:
  actual=row_counts(db)
  if not m.get('row_counts') or any(actual.get(table)!=count for table,count in m['row_counts'].items()):raise ValueError('Restored row counts differ from source')
 counts=output(COMPOSE+['exec','-T','postgres','psql','-U','postgres','-d',db,'-Atc',"SELECT 'places='||count(*) FROM places UNION ALL SELECT 'visits='||count(*) FROM visits UNION ALL SELECT 'photos='||count(*) FROM visit_photos"])
 with tempfile.TemporaryDirectory(prefix='vandrouka-restore-') as directory:
  with tarfile.open(source/'minio.tar.gz') as archive:
   members=archive.getmembers()
   for member in members:
    dest=(pathlib.Path(directory)/member.name).resolve()
    if not(dest.is_relative_to(pathlib.Path(directory)) and not member.issym() and not member.islnk() and (member.isfile() or member.isdir())):raise ValueError('Unsafe archive member')
   archive.extractall(directory,members=members,filter='data')
   print('PASS: isolated database restored;',counts.replace('\n','; '),'; storage archive restored:',len(members),'entries')
finally:
 run(COMPOSE+['exec','-T','postgres','dropdb','-U','postgres','--if-exists',db],stdout=subprocess.DEVNULL)
