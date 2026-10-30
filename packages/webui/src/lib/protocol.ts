const labels: Record<string,string> = {openai:'Chat Completions',responses:'Responses',claude:'Anthropic Messages',gemini:'Gemini'}
export function protocolLabel(format:string, _variant: 'short'|'long' = 'short'):string { return labels[format] ?? format }
