import type { PositionFix } from '../location/locate.ts';
// Virtual origin for inline HTML; no request is made to this host.
export const MAP_DOCUMENT_ORIGIN = 'https://app.vandrouka.local/';

export function isMapDocumentNavigation(url: string): boolean {
  return url === MAP_DOCUMENT_ORIGIN || url === 'about:blank' || url.startsWith('data:text/html');
}

export const MAP_STYLE_URL = 'mapbox://styles/mpaprotski911/cmvfoqaqv00fl01s60qij53my';

export type MapPlace = { id: number; name: string; lat: number; lon: number };
export type MapStage = 'document' | 'library' | 'style' | 'sources';
export type MapMessage = { type: 'progress'; stage: MapStage } | { type: 'ready' } | { type: 'tiles' } | { type: 'tile-status'; rendered: boolean; sourceRequested: boolean; sourceLoaded: boolean } | { type: 'error'; code: 'webgl' | 'load' | 'auth' } | { type: 'select'; id: number };

export function getMapboxToken(): string | null {
  const token = process.env.EXPO_PUBLIC_MAPBOX_ACCESS_TOKEN?.trim();
  return token && /^pk\.[A-Za-z0-9._-]+$/.test(token) ? token : null;
}

export function parseMapMessage(raw: string, places: readonly MapPlace[]): MapMessage | null {
  if (raw.length > 1024) return null;
  try {
    const value = JSON.parse(raw);
    if (!value || typeof value !== 'object') return null;
    if (value.type === 'progress' && ['document', 'library', 'style', 'sources'].includes(value.stage)) return { type: 'progress', stage: value.stage };
    if (value.type === 'tile-status' && typeof value.rendered === 'boolean' && typeof value.sourceRequested === 'boolean' && typeof value.sourceLoaded === 'boolean') return { type: 'tile-status', rendered: value.rendered, sourceRequested: value.sourceRequested, sourceLoaded: value.sourceLoaded };
    if (value.type === 'tiles') return { type: 'tiles' };
    if (value.type === 'ready') return { type: 'ready' };
    if (value.type === 'error' && ['webgl', 'load', 'auth'].includes(value.code)) return { type: 'error', code: value.code };
    if (value.type === 'select' && Number.isSafeInteger(value.id) && places.some(place => place.id === value.id)) return { type: 'select', id: value.id };
  } catch { /* Ignore malformed bridge messages. */ }
  return null;
}

function scriptJSON(value: unknown): string {
  return JSON.stringify(value).replace(/</g, '\\u003c').replace(/\u2028/g, '\\u2028').replace(/\u2029/g, '\\u2029');
}

export function placesScript(places: readonly MapPlace[]): string {
  return `window.setPlaces && window.setPlaces(${scriptJSON(places.map(({ id, name, lat, lon }) => ({ id, name, lat, lon })))}); true;`;
}

export function userLocationScript(position: PositionFix | null): string {
  return `window.setUserLocation && window.setUserLocation(${scriptJSON(position)}); true;`;
}

export function mapDocument(token: string): string {
  if (!/^pk\.[A-Za-z0-9._-]+$/.test(token)) throw new Error('A public Mapbox token is required');
  return `<!doctype html><html lang="ru"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1">
<meta name="referrer" content="no-referrer">
<link rel="stylesheet" href="https://api.mapbox.com/mapbox-gl-js/v3.33.0/mapbox-gl.css" media="print" onload="this.media='all'">
<style>html,body,#map{margin:0;width:100%;height:100%;background:#F7F4EC} .pin{width:44px;height:44px;border:0;border-radius:50%;background:#173F34;color:#fff;font-size:22px;box-shadow:0 2px 7px #0005;cursor:pointer}</style>
</head><body><div id="map" role="region" aria-label="Места Минска на карте"></div>
<script>
(function(){
  var userMarker=null, lastLocationTimestamp=null;
  var rendered=false, sourceRequested=false, sourceLoaded=false;
  var map, markers=[], ready=false, lastMessage={type:'progress',stage:'document'};
  function send(message){if(message.type!=='select') lastMessage=message;window.ReactNativeWebView && window.ReactNativeWebView.postMessage(JSON.stringify(message));}
  window.reportTileStatus=function(){window.ReactNativeWebView && window.ReactNativeWebView.postMessage(JSON.stringify({type:'tile-status',rendered:rendered,sourceRequested:sourceRequested,sourceLoaded:sourceLoaded}));};
  window.reportMapStatus=function(){window.ReactNativeWebView && window.ReactNativeWebView.postMessage(JSON.stringify(lastMessage));};
  send(lastMessage);
  window.mapFailed=function(){send({type:'error',code:'load'});};
  window.setPlaces=function(places){
    if(!ready || !Array.isArray(places)) return;
    markers.forEach(function(marker){marker.remove();}); markers=[];
    places.forEach(function(place){
      var button=document.createElement('button'); button.className='pin'; button.type='button';
      button.textContent='●'; button.setAttribute('aria-label',place.name); button.title=place.name;
      button.addEventListener('click',function(event){event.stopPropagation();send({type:'select',id:place.id});});
      markers.push(new mapboxgl.Marker({element:button}).setLngLat([place.lon,place.lat]).addTo(map));
    });
  };
  window.setUserLocation=function(position){
    if(userMarker){userMarker.remove();userMarker=null;}
    if(!position){lastLocationTimestamp=null;return;}
    if(!ready)return;
    var marker=document.createElement('div');
    marker.style.cssText='width:18px;height:18px;background:#3778A7;border:3px solid white;border-radius:50%;box-shadow:0 0 0 8px #3778a733';
    marker.setAttribute('aria-label','Ваше положение на момент запроса');
    userMarker=new mapboxgl.Marker({element:marker}).setLngLat([position.longitude,position.latitude]).addTo(map);
    if(position.timestamp!==lastLocationTimestamp){map.flyTo({center:[position.longitude,position.latitude],zoom:15});lastLocationTimestamp=position.timestamp;}
  };
  window.startMap=function(){
    send({type:'progress',stage:'library'});
    if(!window.mapboxgl || !mapboxgl.supported()){send({type:'error',code:'webgl'});return;}
    try{
      map=new mapboxgl.Map({container:'map',accessToken:${scriptJSON(token)},style:${scriptJSON(MAP_STYLE_URL)},center:[27.5615,53.9023],zoom:12,attributionControl:true});
      send({type:'progress',stage:'style'});
      window.resizeMap=function(){map.resize();};
      map.addControl(new mapboxgl.NavigationControl({showCompass:false}),'top-right');
      map.on('render',function(){rendered=true;});
      map.on('sourcedataloading',function(){sourceRequested=true;});
      map.on('sourcedata',function(event){if(event.isSourceLoaded) sourceLoaded=true;});
      map.on('styledata',function(){if(!ready) send({type:'progress',stage:'sources'});});
      map.on('style.load',function(){ready=true;map.resize();send({type:'ready'});});
      map.on('load',function(){send({type:'tiles'});});
      map.on('error',function(event){var status=event.error && event.error.status;send({type:'error',code:status===401||status===403?'auth':'load'});});
      map.on('webglcontextlost',function(){send({type:'error',code:'webgl'});});
      window.addEventListener('resize',function(){map.resize();});
      window.addEventListener('pagehide',function(){map.remove();});
    }catch(error){send({type:'error',code:'load'});}
  };
})();
</script>
<script src="https://api.mapbox.com/mapbox-gl-js/v3.33.0/mapbox-gl.js" onload="startMap()" onerror="mapFailed()"></script>
</body></html>`;
}
