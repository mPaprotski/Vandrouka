#!/usr/bin/env python3
"""Consistent local Compose backup; stops API briefly, restarts it in finally.
Private output must be outside the repository. PostgreSQL/MinIO volumes are read only.
"""
import argparse,hashlib,json,os,pathlib,subprocess,shutil,tempfile
ROOT=pathlib.Path(__file__).resolve().parents[1]
COMPOSE=['docker','compose','--env-file','backend/.env']
def run(args,**kwargs):return subprocess.run(args,cwd=ROOT,check=True,**kwargs)
def output(args):return subprocess.check_output(args,cwd=ROOT,text=True).strip()
def digest(p):
 h=hashlib.sha256()
 with p.open('rb') as f:
  for block in iter(lambda:f.read(1024*1024),b''):h.update(block)
 return h.hexdigest()
def row_counts(database):
 tables=['places','users','sessions','session_families','visits','visit_photos','xp_transactions','media_deletions','user_achievements']
 existing=set(output(COMPOSE+['exec','-T','postgres','psql','-U','postgres','-d',database,'-Atc',"SELECT tablename FROM pg_tables WHERE schemaname='public'"]).splitlines())
 tables=[table for table in tables if table in existing]
 fields=','.join("'%s',(SELECT count(*) FROM %s)"%(table,table) for table in tables)
 return json.loads(output(COMPOSE+['exec','-T','postgres','psql','-U','postgres','-d',database,'-Atc','SELECT json_build_object('+fields+')']))
def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('directory');args=p.parse_args();target=pathlib.Path(args.directory).resolve()
 if target.is_relative_to(ROOT) or target.exists():p.error('Choose a new private directory outside repository')
 target.parent.mkdir(parents=True,exist_ok=True)
 stage=pathlib.Path(tempfile.mkdtemp(prefix='.vandrouka-backup-',dir=target.parent));os.chmod(stage,0o700)
 api=output(COMPOSE+['ps','--status','running','-q','api']);stopped=False
 try:
  minio=output(COMPOSE+['ps','-q','minio'])
  if not minio:raise RuntimeError('MinIO not running')
  mounts=json.loads(output(['docker','inspect',minio]))[0]['Mounts'];volume=next(m['Name'] for m in mounts if m['Destination']=='/data' and m['Type']=='volume')
  if api:
   stopped=True;run(COMPOSE+['stop','api'],stdout=subprocess.DEVNULL)
  with (stage/'postgres.dump').open('wb') as f:run(COMPOSE+['exec','-T','postgres','pg_dump','-U','postgres','-d','vandrouka','-Fc'],stdout=f)
  with (stage/'minio.tar.gz').open('wb') as f:run(['docker','run','--rm','--network','none','--mount',f'type=volume,src={volume},dst=/source,readonly','alpine:3.20','tar','-czf','-','-C','/source','.'],stdout=f)
  manifest={'version':2,'row_counts':row_counts('vandrouka'),'files':{name:digest(stage/name) for name in ['postgres.dump','minio.tar.gz']},'note':'Private full local snapshot. Restore only into isolation; old backups contain deleted personal data.'}
  (stage/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n');stage.rename(target);print('Backup created:',target)
 finally:
  if stopped:run(COMPOSE+['start','api'],stdout=subprocess.DEVNULL)
  if stage.exists():shutil.rmtree(stage)
if __name__=='__main__':main()
