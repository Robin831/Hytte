export type NamedGreetingKey = 'greeting.morningNamed' | 'greeting.afternoonNamed' | 'greeting.eveningNamed'
export type UnnamedGreetingKey = 'greeting.morning' | 'greeting.afternoon' | 'greeting.evening'

export function getGreetingKey(hour: number, named: true): NamedGreetingKey
export function getGreetingKey(hour: number, named: false): UnnamedGreetingKey
export function getGreetingKey(hour: number, named: boolean): NamedGreetingKey | UnnamedGreetingKey {
  if (hour < 12) return named ? 'greeting.morningNamed' : 'greeting.morning'
  if (hour < 17) return named ? 'greeting.afternoonNamed' : 'greeting.afternoon'
  return named ? 'greeting.eveningNamed' : 'greeting.evening'
}
