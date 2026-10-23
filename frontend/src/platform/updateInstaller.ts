import { tr } from '../locales'

/** Called only after the Go updater verifies the downloaded artifact again. */
export function openAndroidUpdateInstaller(path: string): void {
  const bridge = (window as unknown as {wails?:{installApk?:(path:string)=>string}}).wails
  if (!bridge?.installApk) throw new Error(tr('update.androidHostRequired'))
  const error = bridge.installApk(path)
  if (error) throw new Error(error)
}
