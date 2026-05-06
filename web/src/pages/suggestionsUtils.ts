// nextRunHintKey returns the i18n key for the header next-run text. The
// scheduler fires at 03:00 Europe/Oslo every day. If the next 03:00 is less
// than 12 hours away (i.e. evening/night through pre-03:00 the same morning)
// it is "tonight"; otherwise (daytime hours after the morning run) it is
// "tomorrow".
export function nextRunHintKey(now: Date): 'header.nextRunTonight' | 'header.nextRunTomorrow' {
  const hourStr = new Intl.DateTimeFormat('en-US', {
    timeZone: 'Europe/Oslo',
    hour: 'numeric',
    hour12: false,
  }).format(now)
  // Intl can return "24" for midnight in some runtimes — treat it as 0.
  const hour = parseInt(hourStr, 10) % 24
  // Hours until the next 03:00 Oslo. At 03:00 exactly the next run is 24h away.
  const hoursUntil = ((3 - hour + 24) % 24) || 24
  return hoursUntil < 12 ? 'header.nextRunTonight' : 'header.nextRunTomorrow'
}
