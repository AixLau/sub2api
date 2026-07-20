import { ArrowUpRight } from 'lucide-react'
import { publicPageLinks } from '../data/alwayzz'

export function PublicPageDirectory() {
  return (
    <section className="content-section page-directory-section" aria-labelledby="directory-title">
      <div className="content-inner">
        <div className="section-heading">
          <p>Explore</p>
          <h2 id="directory-title">需要的信息，各有入口。</h2>
          <span>首页介绍产品价值；模型、服务能力、状态和常见问题可分别查看。</span>
        </div>

        <nav className="page-directory-grid" aria-label="产品页面">
          {publicPageLinks.map((page, index) => (
            <a className="page-directory-card" href={page.href} key={page.href}>
              <span className="page-directory-index">{String(index + 1).padStart(2, '0')}</span>
              <div>
                <small>{page.eyebrow}</small>
                <h3>{page.label}</h3>
                <p>{page.description}</p>
              </div>
              <ArrowUpRight aria-hidden="true" size={19} />
            </a>
          ))}
        </nav>
      </div>
    </section>
  )
}
