import energyGiftImage from '@/assets/welcome-reward/energy-gift.jpg'
import luckyPassageImage from '@/assets/welcome-reward/lucky-passage.jpg'
import starlinkExplorerImage from '@/assets/welcome-reward/starlink-explorer.jpg'

export const welcomeRewardSkins = [
  {
    id: 'starlink-explorer',
    coverImage: starlinkExplorerImage,
    coverColor: '#315d98',
    coverTextColor: '#13233d'
  },
  {
    id: 'energy-gift',
    coverImage: energyGiftImage,
    coverColor: '#d8e5dc',
    coverTextColor: '#174b49'
  },
  {
    id: 'lucky-passage',
    coverImage: luckyPassageImage,
    coverColor: '#13263a',
    coverTextColor: '#fff5dd'
  }
] as const

export type WelcomeRewardSkinId = (typeof welcomeRewardSkins)[number]['id']

export function isWelcomeRewardSkinId(value: unknown): value is WelcomeRewardSkinId {
  return welcomeRewardSkins.some((skin) => skin.id === value)
}

export function pickWelcomeRewardSkinId(): WelcomeRewardSkinId {
  let index: number
  if (typeof crypto !== 'undefined' && typeof crypto.getRandomValues === 'function') {
    const randomValue = new Uint32Array(1)
    crypto.getRandomValues(randomValue)
    index = randomValue[0] % welcomeRewardSkins.length
  } else {
    index = Math.floor(Math.random() * welcomeRewardSkins.length)
  }
  return welcomeRewardSkins[index].id
}
