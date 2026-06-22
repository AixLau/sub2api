# 可用模型玻璃感卡片组件

## 组件位置
- **组件**: `frontend/src/components/model/AvailableModelsCard.vue`
- **示例页面**: `frontend/src/views/ModelsView.vue`

## 特性

### 玻璃感设计
- 使用 `card-glass` 样式类，具有毛玻璃效果
- 半透明背景 + backdrop-blur 模糊效果
- 支持深色模式自动适配

### 响应式布局
- 卡片网格自动适配屏幕尺寸
- 悬停效果：卡片上移 + 阴影增强
- 提供商图标悬停时放大

### 功能特点
1. **加载状态**: 显示 LoadingSpinner
2. **空状态**: 友好的空状态提示
3. **折叠展开**: 默认显示前 N 个，支持展开查看全部
4. **状态徽章**: active/beta/deprecated 状态标识
5. **标签系统**: 显示模型特性标签（context、vision 等）
6. **定价信息**: 展示输入/输出 token 价格

## 使用方法

### 基础用法

```vue
<template>
  <AvailableModelsCard
    :models="models"
    :loading="loading"
  />
</template>

<script setup lang="ts">
import AvailableModelsCard from '@/components/model/AvailableModelsCard.vue'
import type { ModelInfo } from '@/components/model/AvailableModelsCard.vue'

const models = ref<ModelInfo[]>([
  {
    id: 'claude-opus-4',
    name: 'Claude Opus 4',
    provider: 'Anthropic',
    status: 'active',
    tags: ['200K context', 'Vision'],
    pricing: { input: 15, output: 75 }
  }
])
</script>
```

### Props

| 属性 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `title` | `string` | `'Available Models'` | 卡片标题 |
| `models` | `ModelInfo[]` | `[]` | 模型列表 |
| `loading` | `boolean` | `false` | 加载状态 |
| `initialDisplayCount` | `number` | `6` | 初始显示数量 |

### ModelInfo 类型

```typescript
interface ModelInfo {
  id: string                    // 模型唯一标识
  name: string                  // 模型名称
  provider: string              // 提供商（Anthropic/OpenAI/Google等）
  status?: 'active' | 'beta' | 'deprecated'  // 状态
  tags?: string[]              // 特性标签
  pricing?: {
    input?: number             // 输入价格（$/M tokens）
    output?: number            // 输出价格（$/M tokens）
  }
}
```

## 样式定制

### 提供商颜色映射

组件内置了主流 AI 提供商的配色：
- **Anthropic/Claude**: 琥珀色
- **OpenAI**: 翠绿色
- **Google/Gemini**: 蓝色
- **Meta**: 紫色
- **Mistral**: 橙色

可以通过修改 `providerColorClass` 函数来自定义颜色。

### 状态颜色

- **active**: 绿色（正常可用）
- **beta**: 蓝色（测试版）
- **deprecated**: 灰色（已弃用）

## 国际化

支持中英文：

**中文**:
- `models.availableModels`: 可用模型
- `models.noModelsAvailable`: 暂无可用模型
- `common.showMore`: 显示更多

**英文**:
- `models.availableModels`: Available Models
- `models.noModelsAvailable`: No models available
- `common.showMore`: Show More

## 示例截图

卡片展示效果：
- 玻璃感半透明背景
- 提供商彩色图标徽章
- 模型名称 + 提供商信息
- 特性标签（灰色小标签）
- 定价信息（输入↓ / 输出↑）
- 状态徽章（右上角）

## 集成到现有页面

如果要集成到管理后台 Dashboard：

```vue
<template>
  <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
    <!-- 左侧：现有图表 -->
    <ActiveUsersTrend :trend-data="activeUsersTrend" :loading="loading" />
    
    <!-- 右侧：可用模型卡片 -->
    <AvailableModelsCard
      :title="t('models.availableModels')"
      :models="models"
      :loading="modelsLoading"
      :initial-display-count="6"
    />
  </div>
</template>
```

## 注意事项

1. **数据获取**: 需要自行实现获取模型列表的 API 调用
2. **权限控制**: 根据用户角色显示不同的模型列表
3. **实时更新**: 如需实时更新模型状态，可使用 WebSocket 或轮询
4. **性能优化**: 大量模型时建议使用虚拟滚动或分页
