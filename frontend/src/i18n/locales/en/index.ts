import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelMonitorV2 from './channelMonitorV2'
import batchImage from './batchImage'
import admin from './admin'
import misc from './misc'
import extensions from './extensions'
import merchant from './merchant'
import { mergeLocaleMessages } from '../merge'

export default mergeLocaleMessages({
  ...landing,
  ...common,
  ...dashboard,
  ...channelMonitorV2,
  ...batchImage,
  admin,
  ...misc,
  ...merchant,
}, extensions)
