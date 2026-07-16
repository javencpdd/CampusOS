<template>
  <section class="pack-manager" aria-labelledby="homepage-pack-heading">
    <div class="section-heading">
      <div>
        <p class="eyebrow">Homepage Resource Package</p>
        <h3 id="homepage-pack-heading">首页风格包</h3>
        <p>管理员统一选择首页资源包。导入的 zip 必须先通过服务端筛查，应用失败时可以回滚。</p>
      </div>
      <el-button type="warning" plain :loading="rollingBack" @click="rollbackPack">回滚当前首页</el-button>
    </div>

    <div class="pack-controls">
      <el-select
        v-model="selectedPack"
        filterable
        :loading="packsLoading"
        placeholder="选择已校验的首页资源包"
      >
        <el-option
          v-for="pack in packs"
          :key="pack.name"
          :label="`${pack.display_name || pack.name}${pack.version ? ` v${pack.version}` : ''}`"
          :value="pack.name"
          :disabled="!pack.validation?.valid"
        />
      </el-select>
      <el-button :loading="packsLoading" @click="loadPacks">刷新目录</el-button>
      <el-button
        type="primary"
        :disabled="!selectedPack"
        :loading="applyingPack"
        @click="applySourcePack"
      >切换首页包</el-button>
      <input
        ref="packInput"
        class="hidden"
        type="file"
        accept=".zip,application/zip"
        @change="selectPackFile"
      />
      <el-button @click="packInput?.click()">选择 zip</el-button>
      <el-button :disabled="!packFile" :loading="validatingPack" @click="validatePack">筛查</el-button>
      <el-button
        type="primary"
        :disabled="!packFile || validation?.valid !== true"
        :loading="uploadingPack"
        @click="applyPack"
      >导入并应用</el-button>
      <el-button :loading="downloadingExample" @click="downloadExample">导出当前示例</el-button>
    </div>

    <p v-if="packFile" class="file-name">待导入：{{ packFile.name }}</p>
    <el-alert
      v-if="validation"
      :type="validation.valid ? 'success' : 'error'"
      :closable="false"
      show-icon
      :title="validation.valid ? '资源包筛查通过，可以应用' : `筛查失败：${(validation.errors || []).join('；')}`"
    />
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { homeStylePackApi } from '@/modules/appearance/api'

const packs = ref<any[]>([])
const packsLoading = ref(false)
const selectedPack = ref('')
const applyingPack = ref(false)
const rollingBack = ref(false)
const packInput = ref<HTMLInputElement | null>(null)
const packFile = ref<File | null>(null)
const validation = ref<any>(null)
const validatingPack = ref(false)
const uploadingPack = ref(false)
const downloadingExample = ref(false)

const unwrap = (payload: any) => payload?.data || payload

const loadPacks = async () => {
  packsLoading.value = true
  try {
    packs.value = unwrap(await homeStylePackApi.sources())?.items || []
    if (!packs.value.some((pack) => pack.name === selectedPack.value && pack.validation?.valid)) {
      selectedPack.value = packs.value.find((pack) => pack.validation?.valid)?.name || ''
    }
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载首页资源包失败')
  } finally {
    packsLoading.value = false
  }
}

const applySourcePack = async () => {
  applyingPack.value = true
  try {
    await homeStylePackApi.applySource(selectedPack.value)
    ElMessage.success('首页资源包已切换')
  } catch (error: any) {
    ElMessage.error(error?.msg || '应用资源包失败')
  } finally {
    applyingPack.value = false
  }
}

const selectPackFile = (event: Event) => {
  packFile.value = (event.target as HTMLInputElement).files?.[0] || null
  validation.value = null
}

const validatePack = async () => {
  if (!packFile.value) return
  validatingPack.value = true
  try {
    validation.value = unwrap(await homeStylePackApi.validate(packFile.value))?.validation
  } catch (error: any) {
    ElMessage.error(error?.msg || '资源包筛查失败')
  } finally {
    validatingPack.value = false
  }
}

const applyPack = async () => {
  if (!packFile.value || !validation.value?.valid) return
  uploadingPack.value = true
  try {
    await homeStylePackApi.apply(packFile.value)
    ElMessage.success('首页资源包已导入并应用')
    await loadPacks()
  } catch (error: any) {
    ElMessage.error(error?.msg || '导入资源包失败')
  } finally {
    uploadingPack.value = false
  }
}

const rollbackPack = async () => {
  rollingBack.value = true
  try {
    await homeStylePackApi.rollback()
    ElMessage.success('首页风格已回滚')
  } catch (error: any) {
    ElMessage.error(error?.msg || '回滚失败')
  } finally {
    rollingBack.value = false
  }
}

const downloadExample = async () => {
  downloadingExample.value = true
  try {
    const payload: any = await homeStylePackApi.exampleZip()
    const blob = payload instanceof Blob ? payload : new Blob([payload], { type: 'application/zip' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'homepage-style-pack.zip'
    link.click()
    URL.revokeObjectURL(url)
  } catch (error: any) {
    ElMessage.error(error?.msg || '导出示例失败')
  } finally {
    downloadingExample.value = false
  }
}

onMounted(loadPacks)
</script>

<style scoped>
.pack-manager {
  display: grid;
  gap: 16px;
  padding: 18px;
  border: 1px solid #e4e7ed;
  background: #fff;
}

.section-heading,
.pack-controls {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.section-heading {
  align-items: flex-start;
}

.section-heading h3,
.section-heading p {
  margin: 0;
}

.section-heading > div > p:last-child {
  margin-top: 7px;
  color: #606266;
  line-height: 1.6;
}

.eyebrow {
  margin-bottom: 5px !important;
  color: #7c3aed;
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
}

.pack-controls {
  justify-content: flex-start;
  flex-wrap: wrap;
}

.pack-controls .el-select {
  width: min(360px, 100%);
}

.file-name {
  margin: 0;
  color: #606266;
  font-size: 13px;
}

.hidden {
  display: none;
}

@media (max-width: 760px) {
  .section-heading {
    align-items: stretch;
    flex-direction: column;
  }

  .pack-controls > * {
    width: 100%;
  }
}
</style>
