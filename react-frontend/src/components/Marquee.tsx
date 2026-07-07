type MarqueeProps<T> = {
  items: T[]
  repetitions?: number
  className?: string
  renderItem: (item: T, index: number) => React.ReactNode
}

export function Marquee<T>({ items, repetitions = 4, className = '', renderItem }: MarqueeProps<T>) {
  const repeatedItems = Array.from({ length: repetitions }).flatMap(() => items)

  return (
    <div className={`marquee ${className}`}>
      <div className="marquee-track">
        {repeatedItems.map((item, index) => (
          <div className="marquee-item" key={`${index}-${String(item)}`}>
            {renderItem(item, index)}
          </div>
        ))}
      </div>
    </div>
  )
}
