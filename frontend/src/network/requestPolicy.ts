export const NETWORK_POLICY_KEY = 'network_request_policy_v1'
export const networkFrequencies = ['standard', 'steady', 'low'] as const
export type NetworkFrequency = typeof networkFrequencies[number]
export function decodeNetworkPolicy(raw: unknown): { frequency: NetworkFrequency } {
 if (raw === '' || raw === undefined) return {frequency:'standard'}
 const value: unknown = typeof raw === 'string' ? JSON.parse(raw) : raw
 if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid network policy')
 const setting = value as Record<string,unknown>
 const frequency = setting.frequency === undefined ? 'standard' : networkFrequencies.find(value=>value===setting.frequency)
 if (!frequency) throw new Error('Invalid network frequency')
 return {frequency}
}
export function networkRetryDelay(frequency:NetworkFrequency, retryAfterSeconds=0):number {
 if (Number.isFinite(retryAfterSeconds)&&retryAfterSeconds>0) return Math.ceil(retryAfterSeconds)*1000
 const [minimum,jitter]=frequency==='low'?[1800,300]:frequency==='steady'?[600,120]:[300,60]
 return (minimum+Math.floor(Math.random()*(jitter+1)))*1000
}
