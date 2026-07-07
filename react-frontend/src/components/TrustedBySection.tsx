import { trustedCompanies } from '../data/alwayzz'
import { Marquee } from './Marquee'

export function TrustedBySection() {
  return (
    <section className="trusted-section" aria-label="服务能力">
      <div className="trusted-inner">
        <p className="trusted-label">Partnered with top-tier companies globally</p>
        <Marquee
          className="trusted-marquee"
          items={trustedCompanies}
          renderItem={(company) => <span className={company.className}>{company.name}</span>}
        />
      </div>
    </section>
  )
}
