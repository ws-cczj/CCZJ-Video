"""Shared video RPC checks for isolated Windows and Android acceptance hosts.

Adds one uniquely named disabled source, removes its own test records, and
restores the default-source preference. Never clears an application profile.
"""
import base64,json,re,shlex,subprocess,uuid
from pathlib import Path


def probe_video_domain(rpc,check_export=None):
    token=uuid.uuid4().hex[:18]
    key='audit_video_'+token
    identity={'source_key':key,'vod_id':'fixture'}
    history=dict(identity,ep_num=1,position=42.5)
    previous=rpc('GetSetting',['default_source_key'])
    created=False
    try:
        rpc('CreateSource',[{'source_key':key,'name':'Video domain verification','api_url':'https://example.invalid/audit','kind':'video','enabled':0}])
        created=True
        video={'vod_id':'fixture','vod_name':'Audit video '+token,'type_id':'audit_type','type_name':'Audit category','vod_remarks':'audit remarks'}
        assert rpc('ImportSourceVideos',[key,[video]])==1
        rows=rpc('GetVideoList',[{'source_key':key,'page':1,'page_size':20}])
        assert rows['total']==1 and rows['videos'][0]['vod_id']=='fixture',rows
        inspection=rpc('GetSourceDetail',[key])
        assert next(t['row_count'] for t in inspection['tables'] if t['table_name']=='source_videos')==1,inspection
        stats=next(s for s in rpc('GetSourceStats',[]) if s['source_key']==key)
        assert stats['video_count']==1,stats
        bundle={'version':2,'source':{'source_key':key,'name':'Video domain verification','api_url':'https://example.invalid/audit','kind':'video','enabled':0},'videos':[dict(video,source_vod_id='fixture')],'types':[]}
        encoded=base64.b64encode(json.dumps(bundle).encode('utf-8')).decode('ascii')
        assert 'imported' in rpc('ImportSourceFromBase64',['audit.json',encoded])
        assert rpc('GetVideoList',[{'source_key':key,'page':1,'page_size':20}])['total']==1
        if check_export:
            first=rpc('ExportSource',[key]);second=rpc('ExportSource',[key])
            assert first!=second,'repeated native export reused its path'
            check_export(first,key);check_export(second,key)
        rpc('AddFavorite',[identity])
        assert rpc('IsFavorite',[identity]) is True
        assert any(r['source_key']==key for r in (rpc('GetFavorites',[1,1000]) or []))
        rpc('SaveWatchHistory',[history])
        assert rpc('GetHistoryPosition',[history])==42.5
        assert rpc('GetWatchedEpisodes',[identity])==[1]
        recent=rpc('GetRecentHistory',[1000])
        row=next(r for r in recent if r['source_key']==key)
        assert row['vod_remarks']=='audit remarks',row
        if check_export:
            backup_paths=[]
            try:
                for _ in range(2): backup_paths.append(rpc('BackupExport',['']))
                assert backup_paths[0]!=backup_paths[1],'repeated backup export reused its path'
                name=Path(backup_paths[-1]).name
                assert any(item['name']==name for item in rpc('BackupExports',[]))
                restored=rpc('BackupImportFile',[name])
                assert isinstance(restored,dict),restored
                assert rpc('IsFavorite',[identity]) is True
                assert rpc('GetHistoryPosition',[history])==42.5
            finally:
                for path in backup_paths: check_export(path,'$backup')
        global_id=rpc('GetGlobalIdForVideo',[key,'fixture'])
        assert any(r['source_key']==key for r in rpc('FindSourcesByGlobalId',[global_id]))
        rpc('DeleteVideo',[identity])
        assert not any(r['source_key']==key for r in (rpc('GetFavorites',[1,1000]) or []))
        recycle=rpc('GetRecycleBin',[{'source_key':key,'page':1,'page_size':50}])
        assert recycle['total']==1 and recycle['items'][0]['vod_id']=='fixture',recycle
        rpc('RestoreVideo',[identity])
        assert rpc('GetHistoryPosition',[history])==42.5
        for action in ['PurgeVideo','ClearRecycleBin']:
            rpc('DeleteVideo',[identity])
            args=identity if action=='PurgeVideo' else {'source_key':key}
            assert rpc(action,[args])['affected']==1
            assert not any(r['source_key']==key for r in (rpc('GetFavorites',[1,1000]) or []))
            assert not any(r['source_key']==key for r in (rpc('GetRecentHistory',[1000]) or []))
            assert rpc('GetRecycleBin',[{'source_key':key}])['total']==0
            assert rpc('ImportSourceVideos',[key,[video]])==1
            assert rpc('IsFavorite',[identity]) is True
            assert rpc('GetHistoryPosition',[history])==42.5
        for action in ['truncate','recreate']:
            assert 'ok' in rpc('RunSourceAction',[{'source_key':key,'action':action}])
            assert not any(r['source_key']==key for r in (rpc('GetFavorites',[1,1000]) or []))
            assert not any(r['source_key']==key for r in (rpc('GetRecentHistory',[1000]) or []))
            assert rpc('ImportSourceVideos',[key,[video]])==1
            assert rpc('IsFavorite',[identity]) is True
            assert rpc('GetHistoryPosition',[history])==42.5
        rpc('DeleteHistoryItem',[history])
        assert rpc('GetHistoryPosition',[history])==0
        rpc('RemoveFavorite',[identity])
        assert rpc('IsFavorite',[identity]) is False
        return {'passed':True,'scope':'native RPC bundle and backup import/export/list/identity/favorites/history/delete/restore/purge/clear/source-truncate/recreate/reimport with SQLite; no remote detail or playback','exports':bool(check_export),'backup_roundtrip':bool(check_export),'source':key}
    finally:
        try:
            if created:
                try:
                    rpc('RemoveFavorite',[identity])
                    rpc('DeleteHistoryByVideo',[identity])
                finally:
                    rpc('DeleteSource',[key])
        finally:
            rpc('SetSetting',['default_source_key',previous or ''])


def probe_android_video_domain(view):
    source=Path('frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8')
    ids={m[1]:int(m[2]) for m in re.finditer(r'export function (\w+)\([^)]*\) \{\s*return \$Call.ByID\((\d+)',source)}
    view.evaluate('''(() => {
      const previous=window._wailsAndroidCallback,pending=new Map();let seq=0;
      window._wailsAndroidCallback=(id,response,error)=>{
        if(!pending.has(id)){previous?.(id,response,error);return}
        const done=pending.get(id);pending.delete(id);
        try{done(error?{error}:JSON.parse(response))}catch(e){done({error:String(e)})}
      };
      window.__videoAudit={call(methodID,args){
        const id='video-audit-'+(++seq),done=new Promise(resolve=>pending.set(id,resolve));
        wails.invokeAsync(id,JSON.stringify({object:0,method:0,args:{methodID,args,'call-id':id}}));return done;
      },dispose(){window._wailsAndroidCallback=previous;delete window.__videoAudit}};
    })()''')
    def rpc(name,args):
        reply=view.evaluate(f'__videoAudit.call({ids[name]},{json.dumps(args)})')
        assert reply.get('ok'),(name,reply)
        return reply.get('data',reply.get('text'))
    def check_export(path,key):
        package='com.cczj.video.acceptance'
        pattern=r'backup_[0-9_]+\.json\.br' if key=='$backup' else 'source_'+key+r'_[0-9_]+\.json\.br'
        assert path.startswith(('/data/user/0/'+package+'/', '/data/data/'+package+'/')) and re.fullmatch(pattern,Path(path).name),path
        command=view.adb+['shell','run-as',package]
        subprocess.run(command+['test','-s',shlex.quote(path)],check=True,timeout=10)
        subprocess.run(command+['rm',shlex.quote(path)],check=True,timeout=10)
    try:
        return probe_video_domain(rpc,check_export)
    finally:
        view.evaluate('__videoAudit.dispose()')
