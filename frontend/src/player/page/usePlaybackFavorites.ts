import { ref, watch, onBeforeUnmount } from 'vue'
import { AddFavorite, RemoveFavorite, IsFavorite } from '../../api/app'
import { useFavoriteFoldersStore } from '../../stores/favoriteFolders'
import { bumpFavoritesRefresh } from '../../stores/favoritesSync'
import { useErrorStore } from '../../stores/error'
import { tr } from '../../locales'

type FavoriteIdentity={source_key:string;vod_id:string;global_id:number;vod_name:string}
export function usePlaybackFavorites(identity:()=>FavoriteIdentity) {
  const isFav=ref(false),favBusy=ref(false),showFavFolderModal=ref(false)
  const errors=useErrorStore(),folderStore=useFavoriteFoldersStore()
  let disposed=false,queryGeneration=0
  let pending: (Promise<unknown>&{cancel?:()=>void|Promise<void>})|undefined
  const key=(value:FavoriteIdentity)=>`${value.source_key}-${value.vod_id}`
  const current=(value:FavoriteIdentity)=>!disposed&&key(value)===key(identity())
  function cancelQuery():void {try{void Promise.resolve(pending?.cancel?.()).catch(()=>{})}catch{/* Settled request. */}pending=undefined}
  watch(()=>key(identity()),()=>{queryGeneration++;cancelQuery();isFav.value=false;showFavFolderModal.value=false})
  async function refreshFav():Promise<void> {
    const captured=identity(),token=++queryGeneration
    cancelQuery()
    if(!captured.vod_id||disposed)return
    try{
      const request=IsFavorite(captured)
      pending=request
      const result=await request
      if(current(captured)&&token===queryGeneration)isFav.value=result===true
    }catch{/* A failed query must not overwrite known state. */}
    finally{if(token===queryGeneration)pending=undefined}
  }
  async function toggleFavorite():Promise<void> {
    const captured=identity()
    if(!captured.vod_id||favBusy.value||disposed)return
    if(!isFav.value){showFavFolderModal.value=true;return}
    favBusy.value=true
    try{
      await RemoveFavorite(captured)
      await folderStore.load();delete folderStore.mapping[key(captured)];await folderStore.persistMapping()
      if(current(captured))isFav.value=false
      bumpFavoritesRefresh()
    }catch(error){if(!disposed)errors.fromError(tr('favorites.saveFailed'),error,'Player.favorite')}
    finally{favBusy.value=false}
  }
  async function confirmAddToFolder(folderId:string):Promise<void> {
    const captured=identity()
    if(!captured.vod_id||favBusy.value||disposed)return
    favBusy.value=true
    try{
      await folderStore.load()
      await AddFavorite(captured)
      if(current(captured))isFav.value=true
      folderStore.mapping[key(captured)]=folderId
      if(!await folderStore.persistMapping())return
      if(current(captured))showFavFolderModal.value=false
      if(current(captured))isFav.value=true
      bumpFavoritesRefresh()
    }catch(error){if(!disposed)errors.fromError(tr('favorites.saveFailed'),error,'Player.favorite')}
    finally{favBusy.value=false}
  }
  onBeforeUnmount(()=>{disposed=true;queryGeneration++;cancelQuery()})
  return {isFav,favBusy,showFavFolderModal,refreshFav,toggleFavorite,confirmAddToFolder}
}
