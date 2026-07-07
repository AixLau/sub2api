const sideLines = Array.from({ length: 20 }, (_, index) => ({
  width: 60 + index * 10,
  delay: index * 0.25,
}))

const topLines = Array.from({ length: 20 }, (_, index) => ({
  width: 72 + index * 12,
  delay: index * 0.25,
}))

export function HeroCurveLines() {
  return (
    <>
      <div className="curve-lines curve-lines--left" aria-hidden="true">
        {sideLines.map((line, index) => (
          <span
            className="curve-line curve-line--left"
            key={`left-${line.width}`}
            style={{
              width: `${line.width}px`,
              animationDelay: `${line.delay}s`,
              top: `${Math.max(0, 48 - index * 1.6)}%`,
            }}
          />
        ))}
      </div>

      <div className="curve-lines curve-lines--right" aria-hidden="true">
        {sideLines.map((line, index) => (
          <span
            className="curve-line curve-line--right"
            key={`right-${line.width}`}
            style={{
              width: `${line.width}px`,
              animationDelay: `${line.delay}s`,
              top: `${Math.max(0, 48 - index * 1.6)}%`,
            }}
          />
        ))}
      </div>

      <div className="curve-lines curve-lines--top" aria-hidden="true">
        {topLines.map((line) => (
          <span
            className="curve-line curve-line--top"
            key={`top-${line.width}`}
            style={{
              width: `${line.width}px`,
              animationDelay: `${line.delay}s`,
            }}
          />
        ))}
      </div>
    </>
  )
}
