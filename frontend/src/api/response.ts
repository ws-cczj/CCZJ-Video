import { applicationRequest } from './requestChain'
import { normalizeApiError } from './errors'
import { activityDiagnostics } from '../diagnostics/activity'
import { diagnosticMode } from '../diagnostics/modeScope'

/** Lazy binding invocation keeps input validation and decoding inside the same
 * cancellable composition. No retries or result caching are implicit. */
export function response<T,R>(call:()=>Promise<R>&{cancel?:()=>unknown},decode:(value:R)=>T){
 const finish=activityDiagnostics.begin(`${diagnosticMode()}.transport`)
 const result=applicationRequest(async request=>decode(await request(call())))
 void result.then(()=>finish('success'),error=>{const failure=normalizeApiError(error);finish(failure.code==='CANCELLED'?'cancelled':'failed',failure.code)})
 return result
}
