#!/usr/bin/env python3
"""Read-only external deployment check. No registration, uploads or Mapbox requests."""
import argparse,json,urllib.request,urllib.parse
p=argparse.ArgumentParser();p.add_argument('origin');args=p.parse_args()
u=urllib.parse.urlsplit(args.origin)
if u.scheme!='https' or not u.netloc or u.username or u.password or u.path not in ('','/') or u.query or u.fragment:p.error('Expected HTTPS origin without credentials/path/query')
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*args,**kwargs):return None
client=urllib.request.build_opener(NoRedirect)
for path in ['/health','/v1/cities/minsk/places','/v1/me/progress','/v1/users/me/visits']:
 req=urllib.request.Request(args.origin.rstrip('/')+path,headers={'Accept':'application/json'})
 try:
  with client.open(req,timeout=90) as r:
   data=json.load(r)
   if path=='/health':assert data.get('status')=='ok'
   elif path.endswith('/places'):assert len(data.get('places',[]))>0
   else:raise AssertionError('Private endpoint accessible without authentication')
   print('PASS',path)
 except urllib.error.HTTPError as e:
  if path in ['/v1/me/progress','/v1/users/me/visits'] and e.code==401:
   assert e.headers.get('Cache-Control')=='no-store';print('PASS',path,'401 no-store')
  else:raise
