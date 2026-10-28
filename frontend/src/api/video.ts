import { GetVideoDetail as binding, GetRecommend, GetShortDramaPage as shortDramaBinding, GetVideoList as listBinding } from './app'
import { response } from './response'
import { object,text,integer,each,list,boolean } from './decode'
import type { Video, VideoDetailResponse, VideoListResponse } from '../types'

function decodeCatalogVideo(item:unknown): Video {
 const row=object(item)
 text(row.vod_id)
 for(const key of ['vod_name','type_name','vod_pic','vod_remarks'])if(row[key]!==undefined)text(row[key])
 if(row.global_id!==undefined)integer(row.global_id)
 return row as unknown as Video
}

/** Catalog pages share one representation check before reaching list owners. */
export function GetVideoPage(request:Parameters<typeof listBinding>[0]) {
 return response(()=>listBinding(request),value=>{
  const row=object(value)
  return {videos:list(row.videos,decodeCatalogVideo),total:integer(row.total),next_cursor:row.next_cursor===undefined?'':text(row.next_cursor)} satisfies VideoListResponse
 })
}

/** Network and disk caches share the same representation check. Optional legacy
 * lines/episodes remain optional; absent video is a valid unresolved result. */
function assertDetail(value:unknown):asserts value is VideoDetailResponse{
 const result=object(value)
 if(result.video!=null){const video=object(result.video);for(const key of ['vod_id','vod_name','vod_pic','vod_play_url'])if(video[key]!==undefined)text(video[key])}
 const episode=(value:unknown)=>{const row=object(value);integer(row.ep_num);text(row.ep_url);if(row.ep_name!==undefined)text(row.ep_name)}
 each(result.episodes,episode)
 each(result.lines,value=>{if(value==null)return;const line=object(value);integer(line.index);if(line.name!==undefined)text(line.name);each(line.episodes,episode)})
 if(result.error!==undefined){const failure=object(result.error);text(failure.message)}
 result.video??=null;result.episodes??=[]
}
export function decodeDetail(value:unknown):VideoDetailResponse{assertDetail(value);return value}
export function GetVideoDetail(request:Parameters<typeof binding>[0]){return response(()=>binding(request),decodeDetail)}

export function GetShortDramaRecommendations(sourceKey: string, excludeIDs: string[]) {
 return response(() => GetRecommend({source_key: sourceKey, limit: 5, exclude_ids: excludeIDs, short_drama: true}), value => list(value, decodeCatalogVideo, 5))
}
export function GetShortDramaPage(preferredSource:string, page=1, keyword='', sourceKey='', categoryID='', recentDays=0) {
 return response(()=>shortDramaBinding({preferred_source:preferredSource,source_key:sourceKey,category_id:categoryID,keyword,page,recent_days:recentDays}),value=>{
  const row=object(value),source=text(row.source_key)
  return {source_key:source,source_name:text(row.source_name),category_id:text(row.category_id),total:integer(row.total),has_more:boolean(row.has_more),videos:list(row.videos,decodeCatalogVideo,100).map(video=>({...video,source_key:source}))}
 })
}
