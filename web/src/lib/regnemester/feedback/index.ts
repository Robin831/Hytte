import './feedback.css'

export { soundEngine, type SoundName } from './sound'
export { flashCorrect, flashWrong, flashMilestone } from './flash'
export { vibrate, vibrateCorrect, vibrateWrong, prefersReducedMotion } from './haptics'
export { useFeedback, type UseFeedbackResult } from './useFeedback'
export { emitAchievementUnlock, subscribeAchievementUnlock } from './achievementEvents'
