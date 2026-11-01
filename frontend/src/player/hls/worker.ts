import workerPath from 'hls.js/dist/hls.worker.js?url'

// The ESM build cannot inject the UMD worker. Ship its matching worker asset;
// hls.js owns fallback to inline transmuxing if creation/runtime setup fails.
export const hlsWorkerConfig = { enableWorker: typeof Worker !== 'undefined', workerPath }
