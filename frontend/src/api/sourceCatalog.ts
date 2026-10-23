import { LibraryPreviewSources, LibraryCheckSourceCandidate, LibraryImportSources } from './app'
import { response } from './response'
import { object, text, integer, boolean, each, choice, required } from './decode'
import type { LibraryKind } from './library'

export interface SourceStage { name: string; status: 'ok' | 'failed' | 'skipped'; count?: number; millis: number; message?: string; code?: string }
export interface SourceQuality { status: 'healthy' | 'partial' | 'failed'; score: number; checkedAt: number; millis: number; samples: number; successes: number; stages: SourceStage[]; evidence?:string }
export interface SourceCandidate { id: string; name: string; apiUrl: string; kind: LibraryKind; project: string; origin: string; format: string; compatible: boolean; reason: string; summary: string; imported: boolean; quality?: SourceQuality | null }
export interface SourcePreview { id: string; origin: string; candidates: SourceCandidate[]; expiresAt: number }
export interface SourceImportResult { id: string; sourceKey: string; name: string; status: 'imported' | 'existing' | 'failed'; message: string }
export type SourceStatus = 'unsupported' | 'untested' | 'stale' | SourceQuality['status']
export type SourceCatalogMode = 'bundled' | 'community' | 'custom'

function candidate(value: unknown): SourceCandidate {
  const row = object(value)
  for (const key of ['id', 'name', 'apiUrl', 'project', 'origin', 'format', 'reason', 'summary']) text(row[key])
  choice(row.kind, ['novel', 'comic']); boolean(row.compatible); boolean(row.imported)
  if (row.quality != null) {
    const quality = object(row.quality)
    choice(quality.status, ['healthy', 'partial', 'failed']); integer(quality.score, 0, 100); integer(quality.checkedAt, 1)
    integer(quality.millis); integer(quality.samples, 1); integer(quality.successes, 0, quality.samples as number)
    each(quality.stages, value => { const stage = object(value); text(stage.name); choice(stage.status, ['ok', 'failed', 'skipped']); integer(stage.millis); if(stage.message!==undefined)text(stage.message);return stage })
  }
  return value as SourceCandidate
}

export function previewSources(kind: LibraryKind, input = '', origin = '') {
  return response(() => LibraryPreviewSources(kind, input, origin), value => {
    const row = object(required(value)); text(row.id); text(row.origin); integer(row.expiresAt, 1); each(row.candidates, candidate)
    return row as unknown as SourcePreview
  })
}
export function checkSourceCandidate(previewId: string, id: string, keyword = '') {
  return response(() => LibraryCheckSourceCandidate(previewId, id, keyword), value => candidate(required(value)))
}
export function importSources(previewId: string, ids: string[]) {
  return response(() => LibraryImportSources(previewId, ids), value => {
    each(required(value), value => { const row=object(value);text(row.id);text(row.sourceKey);text(row.name);choice(row.status,['imported','existing','failed']);text(row.message);return row })
    return value as SourceImportResult[]
  })
}

export function sourceHealthStatus(
  compatible: boolean,
  quality: { status: string; checkedAt: number } | null | undefined,
  now = Date.now(),
): SourceStatus {
  if (!compatible) return 'unsupported'
  if (!quality) return 'untested'
  if (now - quality.checkedAt * 1000 > 24 * 3600 * 1000) return 'stale'
  return quality.status === 'healthy' || quality.status === 'partial' || quality.status === 'failed'
    ? quality.status
    : 'failed'
}

export function sourceStatus(candidate: SourceCandidate, now = Date.now()): SourceStatus {
  return sourceHealthStatus(candidate.compatible, candidate.quality, now)
}

/** The bundled tab is an action list: incompatible candidates belong in the
 * community/custom diagnostic views, not among sources users can add. */
export function visibleSourceCandidates(candidates: readonly SourceCandidate[], mode: SourceCatalogMode): SourceCandidate[] {
  return mode === 'bundled' ? candidates.filter(candidate => candidate.compatible) : [...candidates]
}
export function compareSources(a: SourceCandidate, b: SourceCandidate, sort = 'quality', now = Date.now()): number {
  if(sort==='name')return a.name.localeCompare(b.name)
  const rank={healthy:4,partial:3,untested:2,stale:2,failed:1,unsupported:0}
  const evidenceRank=(row:SourceCandidate)=>{
    const status=sourceStatus(row,now)
    if(row.quality?.evidence?.startsWith('benchmark:')){
      if(status==='healthy')return 6
      if(status==='partial'&&row.quality.score>=60)return 5
    }
    return rank[status]
  }
  const difference=evidenceRank(b)-evidenceRank(a)
  if(difference)return difference
  if(sort==='latency')return (a.quality?.millis??Infinity)-(b.quality?.millis??Infinity)||a.name.localeCompare(b.name)
  return (b.quality?.score??-1)-(a.quality?.score??-1)||(a.quality?.millis??Infinity)-(b.quality?.millis??Infinity)||a.name.localeCompare(b.name)
}
