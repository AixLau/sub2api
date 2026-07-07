type AccountField = {
  label: string
  type: 'email' | 'password'
  autoComplete: string
}

type AccountPanel = {
  id: string
  title: string
  fields: AccountField[]
}

const accountPanels: AccountPanel[] = [
  {
    id: 'login',
    title: '登录星链',
    fields: [
      { label: '邮箱', type: 'email', autoComplete: 'email' },
      { label: '密码', type: 'password', autoComplete: 'current-password' },
    ],
  },
  {
    id: 'register',
    title: '创建账号',
    fields: [
      { label: '邮箱', type: 'email', autoComplete: 'email' },
      { label: '密码', type: 'password', autoComplete: 'new-password' },
      { label: '确认密码', type: 'password', autoComplete: 'new-password' },
    ],
  },
  {
    id: 'reset-password',
    title: '找回密码',
    fields: [{ label: '邮箱', type: 'email', autoComplete: 'email' }],
  },
  {
    id: 'change-password',
    title: '修改密码',
    fields: [
      { label: '当前密码', type: 'password', autoComplete: 'current-password' },
      { label: '新密码', type: 'password', autoComplete: 'new-password' },
      { label: '确认新密码', type: 'password', autoComplete: 'new-password' },
    ],
  },
]

export function AccountSection() {
  return (
    <section className="account-section" aria-labelledby="account-title">
      <div className="account-inner">
        <div className="account-heading">
          <p className="account-kicker">Account</p>
          <h2 id="account-title">账号服务</h2>
        </div>

        <div className="account-grid">
          {accountPanels.map((panel) => (
            <article className="account-panel" id={panel.id} key={panel.id}>
              <h3>{panel.title}</h3>
              <form className="account-form">
                {panel.fields.map((field) => {
                  const inputId = `${panel.id}-${field.label}`

                  return (
                    <label className="account-field" htmlFor={inputId} key={inputId}>
                      <span>{field.label}</span>
                      <input id={inputId} type={field.type} autoComplete={field.autoComplete} />
                    </label>
                  )
                })}
                <button className="account-submit" type="button">
                  仅展示，暂不提交
                </button>
              </form>
            </article>
          ))}
        </div>
      </div>
    </section>
  )
}
