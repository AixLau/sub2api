import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import Skeleton from '../Skeleton.vue'

describe('Skeleton', () => {
  it('animates only when motion is allowed', () => {
    const wrapper = mount(Skeleton)

    expect(wrapper.classes()).toContain('motion-safe:animate-shimmer')
    expect(wrapper.classes()).not.toContain('motion-reduce:animate-pulse')
  })
})
