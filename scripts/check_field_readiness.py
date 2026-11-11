#!/usr/bin/env python3
"""Read-only field preflight. Does not start emulators, build APKs or call Mapbox.
Exit 2 means field release prerequisites are missing. --online checks only public API.
"""
import argparse,hashlib,json,pathlib,shutil,subprocess,sys,urllib.parse,zipfile
ROOT=pathlib.Path(__file__).resolve().parents[1]
def main():
 p=argparse.ArgumentParser(description=__doc__)
 p.add_argument('--apk',type=pathlib.Path,help='Standalone Android APK to inspect')
 p.add_argument('--api-origin',help='Public HTTPS API origin; otherwise read mobile/.env.local')
 p.add_argument('--online',action='store_true',help='Run read-only public API check; no Mapbox')
 args=p.parse_args();blocked=[]
 def check(ok,label):
  print(('PASS' if ok else 'BLOCKED')+': '+label)
  if not ok:blocked.append(label)
 package=json.loads((ROOT/'mobile/package.json').read_text());print('Expo:',package['dependencies']['expo'])
 eas=json.loads((ROOT/'mobile/eas.json').read_text());preview=eas.get('build',{}).get('preview',{})
 check(preview.get('android',{}).get('buildType')=='apk' and not preview.get('developmentClient',False),'Preview profile builds a standalone APK')
 origin=args.api_origin
 if origin is None:
  env=ROOT/'mobile/.env.local'
  for line in env.read_text().splitlines() if env.exists() else []:
   if line.startswith('EXPO_PUBLIC_API_URL='):origin=line.split('=',1)[1].strip().strip('\"\'')
 try:
  u=urllib.parse.urlsplit(origin or '')
  host=u.hostname or ''
  public=u.scheme=='https' and bool(host) and not u.username and not u.password and u.path in ('','/') and not u.query and not u.fragment
  import ipaddress
  try:public=public and ipaddress.ip_address(host).is_global
  except ValueError:public=public and host!='localhost' and not host.endswith(('.local','.localhost'))
 except ValueError:public=False
 check(public,'Public HTTPS API origin configured')
 if public:print('API host:',host)
 else:print('Local development address is insufficient for standalone mobile-internet acceptance.')
 if args.apk is None:check(False,'Standalone APK supplied')
 elif not args.apk.is_file():check(False,'APK file exists')
 else:
  try:
   with zipfile.ZipFile(args.apk) as z:
    names=set(z.namelist());valid='AndroidManifest.xml' in names and any(n.startswith('classes') and n.endswith('.dex') for n in names)
  except zipfile.BadZipFile:valid=False
  check(valid,'APK contains Android manifest and dex (not an Expo JS export)')
  if valid:
   h=hashlib.sha256()
   with args.apk.open('rb') as f:
    for chunk in iter(lambda:f.read(1024*1024),b''):h.update(chunk)
   print('APK SHA256:',h.hexdigest())
   signer=shutil.which('apksigner')
   if signer:check(subprocess.run([signer,'verify',str(args.apk)],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0,'APK signature verified')
   else:check(False,'apksigner available to verify APK signature')
   print('APK embedded API URL and runtime behavior still require phone verification.')
 if args.online:
  if public:check(subprocess.run([sys.executable,str(ROOT/'scripts/check_public_api.py'),origin],cwd=ROOT).returncode==0,'External API read-only checks')
  else:print('SKIP: online check; a valid public origin is required')
 print('Manual gate: clean install, permissions, GPS, camera, offline, two accounts; fill docs/field-report-template.md.')
 return 2 if blocked else 0
if __name__=='__main__':raise SystemExit(main())
