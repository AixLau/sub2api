import overview from './overview'
import channels from './channels'
import accounts from './accounts'
import resources from './resources'
import ops from './ops'
import settings from './settings'
import extensions from './extensions'
import { mergeLocaleMessages } from '../../merge'
import audit from './audit'

export default mergeLocaleMessages({
  ...overview,
  ...channels,
  ...accounts,
  ...resources,
  ...ops,
  ...settings,
  ...audit,
}, extensions)
