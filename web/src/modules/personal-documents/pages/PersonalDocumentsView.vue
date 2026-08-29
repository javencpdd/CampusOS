<template>
  <section class="documents-view" v-loading="loading">
    <el-card shadow="never">
      <template #header>
        <div class="header"><div><h2>我的文档</h2><span>文档仅自己可见；每次保存都会生成可恢复的新版本。</span></div><el-button type="primary" @click="createDocument">新建文本</el-button></div>
      </template>
      <div class="toolbar"><el-button @click="chooseUpload">上传文件</el-button><el-button @click="load('active')">我的文档</el-button><el-button @click="load('trashed')">回收站</el-button><input ref="uploadInput" class="hidden" type="file" accept=".txt,.md,.markdown,.campusdoc,.json,.pdf,.docx" @change="upload" /></div>
      <el-alert type="info" :closable="false" show-icon title="文本、Markdown 和 CampusDoc 支持在线编辑；PDF 与 DOCX 保留为私有下载，未配置隔离转换器时不会在服务器中预览。" />
      <el-empty v-if="!items.length" description="暂时没有文档" />
      <el-table v-else :data="items" @row-click="openDocument">
        <el-table-column prop="name" label="名称" min-width="180" /><el-table-column prop="format" label="格式" width="100" /><el-table-column label="版本" width="90"><template #default="{row}">v{{ row.current_version?.version_number || 0 }}</template></el-table-column><el-table-column prop="updated_at" label="更新时间" min-width="160" /><el-table-column label="操作" width="200"><template #default="{row}"><el-button link type="primary" @click.stop="download(row)">下载</el-button><el-button v-if="row.status === 'active'" link type="danger" @click.stop="changeStatus(row, true)">移入回收站</el-button><el-button v-else link type="success" @click.stop="changeStatus(row, false)">恢复</el-button></template></el-table-column>
      </el-table>
    </el-card>
    <el-dialog v-model="editorVisible" :title="editor.name || '文档'" width="min(760px, 94vw)">
      <template v-if="editable">
        <el-input v-model="editor.name" maxlength="255" aria-label="文档名称" />
        <DocumentContentEditor
          :model-value="editor.content || ''"
          :format="editor.format"
          :allow-format-change="!editor.id"
          @update:model-value="editor.content = $event"
          @update:format="changeEditorFormat"
        />
        <div class="editor-actions">
          <el-button v-if="editor.id" @click="preview">安全预览</el-button>
          <el-button v-if="editor.id" @click="showVersions">版本历史</el-button>
        </div>
      </template>
      <el-alert v-else type="info" :closable="false" title="该格式不可在线编辑，请下载后在本地打开。" />
      <template #footer><el-button @click="editorVisible=false">关闭</el-button><el-button v-if="editable" type="primary" :loading="saving" @click="save">保存为新版本</el-button></template>
    </el-dialog>
    <el-dialog v-model="versionsVisible" title="版本历史" width="min(620px, 94vw)"><el-table :data="versions"><el-table-column prop="version_number" label="版本" /><el-table-column prop="created_at" label="创建时间" /><el-table-column label="操作"><template #default="{row}"><el-button link type="primary" @click="restoreVersion(row)">恢复为新版本</el-button></template></el-table-column></el-table></el-dialog>
    <el-drawer v-model="previewVisible" title="文档安全预览" size="min(720px, 94vw)"><!-- previewHTML is generated and sanitized by the API Content Editor Core; never assign editor input directly. --><article class="document-preview" v-html="previewHTML" /></el-drawer>
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { personalDocumentsApi } from '../api'
import DocumentContentEditor from '@/modules/content-editor/components/DocumentContentEditor.vue'
import { defaultDocumentContent, documentNameForFormat, isEditableDocumentFormat, type DocumentFormat } from '@/modules/content-editor/document'
const loading=ref(false), saving=ref(false), items=ref<any[]>([]), status=ref('active'), uploadInput=ref<HTMLInputElement>(), editorVisible=ref(false), versionsVisible=ref(false), versions=ref<any[]>([])
const previewVisible=ref(false), previewHTML=ref('')
const editor=ref<any>({}), editable=computed(()=>isEditableDocumentFormat(editor.value.format))
const data=(r:any)=>r?.data?.data ?? r?.data ?? r
async function load(next=status.value){status.value=next;loading.value=true;try{items.value=data(await personalDocumentsApi.list(next)).items||[]}catch(e:any){ElMessage.error(e?.response?.data?.msg||'加载文档失败')}finally{loading.value=false}}
function createDocument(){editor.value={name:'未命名文档.txt',format:'text',content:defaultDocumentContent('text'),version:0,id:''};editorVisible.value=true}
async function openDocument(row:any){editor.value={...row,content:''};editorVisible.value=true;if(['text','markdown','campusdoc'].includes(row.format)){try{const r=data(await personalDocumentsApi.content(row.id));editor.value={...r.document,content:r.content}}catch(e:any){ElMessage.error(e?.response?.data?.msg||'读取文档失败')}}}
function changeEditorFormat(format:DocumentFormat){editor.value.format=format;editor.value.name=documentNameForFormat(editor.value.name||'未命名文档',format);if(!String(editor.value.content||'').trim())editor.value.content=defaultDocumentContent(format)}
async function save(){saving.value=true;try{if(!editor.value.id){await personalDocumentsApi.create({name:editor.value.name,format:editor.value.format,content:editor.value.content});ElMessage.success('文档已创建')}else{const r=data(await personalDocumentsApi.save(editor.value.id,{expected_version:editor.value.version,name:editor.value.name,content:editor.value.content}));editor.value=r;ElMessage.success('已保存为新版本')}editorVisible.value=false;await load()}catch(e:any){ElMessage.error(e?.response?.data?.msg||'保存失败')}finally{saving.value=false}}
function chooseUpload(){uploadInput.value?.click()}
async function upload(event:Event){const file=(event.target as HTMLInputElement).files?.[0];if(!file)return;try{await personalDocumentsApi.upload(file);ElMessage.success('文件已上传');await load()}catch(e:any){ElMessage.error(e?.response?.data?.msg||'上传失败')}finally{if(uploadInput.value)uploadInput.value.value=''}}
function download(row:any){window.open(personalDocumentsApi.downloadURL(row.id),'_blank','noopener')}
async function changeStatus(row:any,trash:boolean){try{await ElMessageBox.confirm(trash?'文档和全部历史版本都会保留，可在回收站恢复。':'恢复该文档？',trash?'移入回收站':'恢复文档',{type:'warning'});await (trash?personalDocumentsApi.trash(row.id,row.version):personalDocumentsApi.restore(row.id,row.version));await load();ElMessage.success('操作成功')}catch(e:any){if(e!=='cancel'&&e!=='close')ElMessage.error(e?.response?.data?.msg||'操作失败')}}
async function showVersions(){try{versions.value=data(await personalDocumentsApi.versions(editor.value.id)).items||[];versionsVisible.value=true}catch(e:any){ElMessage.error(e?.response?.data?.msg||'读取版本失败')}}
async function restoreVersion(row:any){try{const r=data(await personalDocumentsApi.restoreVersion(editor.value.id,row.id,editor.value.version));editor.value=r;versionsVisible.value=false;ElMessage.success('已恢复为新版本');await load()}catch(e:any){ElMessage.error(e?.response?.data?.msg||'恢复失败')}}
async function preview(){try{const result=data(await personalDocumentsApi.preview(editor.value.id));if(result?.status!=='native'||!result?.rendered_html){ElMessage.info(result?.message||'该文档当前不能在线预览，请下载后查看');return}previewHTML.value=result.rendered_html;previewVisible.value=true}catch(e:any){ElMessage.error(e?.response?.data?.msg||'预览失败')}}
onMounted(()=>load())
</script>
<style scoped>
.documents-view{max-width:1200px;margin:0 auto;padding:20px}.header{display:flex;justify-content:space-between;gap:16px;align-items:center}.header h2{margin:0 0 6px}.header span{color:var(--el-text-color-secondary);font-size:13px}.toolbar{display:flex;gap:8px;margin-bottom:14px}.hidden{display:none}.editor-actions{display:flex;gap:8px}.document-preview{font-size:16px;line-height:1.8;overflow-wrap:anywhere}.document-preview :deep(pre){overflow:auto;padding:12px;background:var(--el-fill-color-light)}.document-preview :deep(table){max-width:100%;border-collapse:collapse}.document-preview :deep(td){padding:6px;border:1px solid var(--el-border-color)}@media(max-width:600px){.documents-view{padding:10px}.header{align-items:flex-start}.header span{display:none}}
</style>
