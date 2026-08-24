import overview from './overview'
import channels from './channels'
import accounts from './accounts'
import resources from './resources'
import ops from './ops'
import settings from './settings'
import extensions from './extensions'
import { mergeLocaleMessages } from '../../merge'
import audit from './audit'
import promptAudit from './promptAudit'
import rewards from './rewards'
import merchant from './merchant'
import plugins from './plugins'

export default mergeLocaleMessages({
  ...overview,
  ...channels,
  ...accounts,
  ...resources,
  ...ops,
  ...settings,
  ...audit,
  ...promptAudit,
  ...rewards,
  ...merchant,
  ...plugins,
}, extensions)
