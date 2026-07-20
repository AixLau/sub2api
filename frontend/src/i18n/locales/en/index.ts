import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import batchImage from './batchImage'
import admin from './admin'
import misc from './misc'
import extensions from './extensions'
import { mergeLocaleMessages } from '../merge'

export default mergeLocaleMessages({
  ...landing,
  ...common,
  ...dashboard,
  ...batchImage,
  admin,
  ...misc,
}, extensions)
