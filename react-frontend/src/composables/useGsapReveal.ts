import { onBeforeUnmount, onMounted, type Ref } from 'vue'
import gsap from 'gsap'
import { ScrollTrigger } from 'gsap/ScrollTrigger'

gsap.registerPlugin(ScrollTrigger)

interface RevealGroup {
  selector: string
  trigger?: string
  start?: string
  y?: number
  scale?: number
  duration?: number
  stagger?: number
  delay?: number
}

export function useGsapReveal(scopeRef: Ref<HTMLElement | null>, groups: RevealGroup[]) {
  let ctx: gsap.Context | undefined

  onMounted(() => {
    const root = scopeRef.value
    const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches

    if (!root || reduceMotion) {
      return
    }

    ctx = gsap.context(() => {
      groups.forEach((group) => {
        const targets = gsap.utils.toArray<HTMLElement>(group.selector)

        if (targets.length === 0) {
          return
        }

        const trigger = group.trigger ? root.querySelector<HTMLElement>(group.trigger) ?? root : root
        let hasPlayed = false

        gsap.set(targets, {
          autoAlpha: 0,
          y: group.y ?? 34,
          scale: group.scale ?? 1,
        })

        const reveal = () => {
          if (hasPlayed) {
            return
          }

          hasPlayed = true
          gsap.to(targets, {
            autoAlpha: 1,
            y: 0,
            scale: 1,
            duration: group.duration ?? 0.82,
            delay: group.delay ?? 0,
            stagger: group.stagger ?? 0.1,
            ease: 'power3.out',
            overwrite: 'auto',
          })
        }

        ScrollTrigger.create({
          trigger,
          start: group.start ?? 'top 72%',
          once: true,
          onEnter: reveal,
          onEnterBack: reveal,
          onLeave: (self) => {
            if (self.direction > 0) {
              reveal()
            }
          },
          onUpdate: (self) => {
            if (self.progress > 0.02) {
              reveal()
            }
          },
        })
      })
    }, root)
  })

  onBeforeUnmount(() => {
    ctx?.revert()
  })
}
