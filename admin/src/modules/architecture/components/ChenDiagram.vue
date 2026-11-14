<template>
  <section class="chen-view" aria-label="Chen 经典 ER 图">
    <div class="chen-controls">
      <div class="chen-selector">
        <label for="chen-focus">当前实体</label>
        <select id="chen-focus" :value="focus" @change="emit('select', ($event.target as HTMLSelectElement).value)">
          <option v-for="entity in options || entities" :key="entity.name" :value="entity.name">{{ entity.title }} · {{ entity.name }}</option>
        </select>
      </div>
      <div class="chen-buttons">
        <button type="button" @click="zoom = Math.max(0.1, zoom - 0.1)" aria-label="缩小关系图">−</button>
        <output aria-label="缩放比例">{{ Math.round(zoom * 100) }}%</output>
        <button type="button" @click="zoom = Math.min(1.5, zoom + 0.1)" aria-label="放大关系图">＋</button>
        <button type="button" @click="fit">适应宽度</button>
        <button type="button" @click="download">导出 SVG</button>
      </div>
    </div>
    <div class="chen-examples">
      <span>查看示例：</span>
      <button type="button" @click="emit('select', 'users')">用户与帖子</button>
      <button type="button" @click="emit('select', 'threads')">标签多值属性</button>
      <button type="button" @click="emit('select', 'ai_model_runs')">执行事件弱实体</button>
    </div>
    <p class="chen-help">点击矩形查看实体详情；图内可横向滚动或缩放。基数标在实体一端，所有连接均不带箭头。</p>
    <div ref="viewport" class="chen-viewport" tabindex="0" aria-label="可滚动的 Chen 关系图">
      <svg ref="drawing" xmlns="http://www.w3.org/2000/svg" :width="diagram.width * zoom" :height="diagram.height * zoom" :viewBox="`0 0 ${diagram.width} ${diagram.height}`" role="img" :aria-label="`${current.title}的Chen实体属性联系图`" data-chen-diagram>
        <title>{{ current.title }} · Chen 实体属性联系图</title>
        <desc>矩形是实体，椭圆是属性，标识符加粗并加下划线，菱形是联系。双线表示该实体全参与；单线表示部分参与。逻辑引用只作说明，不宣称数据库强制约束。</desc>
        <rect width="100%" height="100%" fill="#ffffff" />
        <g stroke="#475569" stroke-width="1.6" fill="none">
          <line v-for="attribute in diagram.attributes" :key="`line:${attribute.id}`" :x1="attribute.start.x" :y1="attribute.start.y" :x2="attribute.end.x" :y2="attribute.end.y" />
          <g v-for="line in diagram.lines" :key="line.id" :data-participation="line.double ? 'total' : 'partial'">
            <line v-for="(segment, index) in parallelLines(line.start, line.end, line.double)" :key="index" :x1="segment.start.x" :y1="segment.start.y" :x2="segment.end.x" :y2="segment.end.y" />
          </g>
        </g>
        <g v-for="line in diagram.lines" :key="`cardinality:${line.id}`" font-family="Arial, sans-serif" font-size="19" font-weight="700" text-anchor="middle" fill="#1e293b">
          <text :x="line.label?.x" :y="line.label?.y" stroke="#fff" stroke-width="6" paint-order="stroke">{{ line.cardinality }}</text>
        </g>
        <g v-for="relationship in diagram.relationships" :key="relationship.relation.id" :data-relationship="relationship.relation.id" :data-identifying="relationship.relation.identifying">
          <title>{{ relationship.relation.detail }}；{{ relationship.relation.basis === 'foreign-key' ? '外键约束' : '已审阅的逻辑引用，非外键' }}</title>
          <polygon :points="diamond(relationship.x, relationship.y, 86, 48)" :fill="relationship.relation.basis === 'foreign-key' ? '#f0f7ff' : '#fff8eb'" stroke="#334155" stroke-width="1.8" />
          <polygon v-if="relationship.relation.identifying" :points="diamond(relationship.x, relationship.y, 78, 40)" fill="none" stroke="#334155" stroke-width="1.6" />
          <text :x="relationship.x" :y="relationship.y + 5" text-anchor="middle" font-family="sans-serif" font-size="16" fill="#1e293b">{{ relationship.relation.name }}</text>
          <text :x="relationship.x" :y="relationship.y + 70" text-anchor="middle" font-family="sans-serif" font-size="12" fill="#64748b">{{ relationship.relation.basis === 'foreign-key' ? '外键约束' : '逻辑引用' }}</text>
        </g>
        <g v-for="entity in diagram.entities" :key="entity.id" class="chen-entity" role="button" tabindex="0" :aria-label="`查看实体 ${entity.table.title} ${entity.table.name}`" :aria-pressed="entity.id === focus" :data-entity="entity.id" :data-weak="entity.weak" @click="emit('select', entity.id)" @keydown.enter.prevent="emit('select', entity.id)" @keydown.space.prevent="emit('select', entity.id)">
          <title>{{ entity.table.title }} · {{ entity.table.name }}</title>
          <rect :x="entity.x - entity.width / 2" :y="entity.y - entity.height / 2" :width="entity.width" :height="entity.height" :fill="entity.id === focus ? '#eaf3ff' : '#ffffff'" :stroke="entity.id === focus ? '#2563eb' : '#334155'" stroke-width="2" />
          <rect v-if="entity.weak" :x="entity.x - entity.width / 2 + 6" :y="entity.y - entity.height / 2 + 6" :width="entity.width - 12" :height="entity.height - 12" fill="none" stroke="#334155" stroke-width="1.6" />
          <text :x="entity.x" :y="entity.y - 13" text-anchor="middle" font-family="sans-serif" font-size="16" font-weight="600" fill="#1e293b">
            <tspan v-for="(part, i) in wrapped(entity.table.title, 15).slice(0, 2)" :key="i" :x="entity.x" :dy="i ? 19 : 0">{{ part }}</tspan>
          </text>
          <text :x="entity.x" :y="entity.y + 28" text-anchor="middle" font-family="monospace" font-size="11" fill="#475569">{{ entity.table.name }}</text>
        </g>
        <g v-for="attribute in diagram.attributes" :key="attribute.id" :data-attribute="attribute.id" :data-key="attribute.key" :data-multivalued="attribute.multivalued" :data-derived="attribute.derived">
          <title>{{ attribute.name }}{{ attribute.partialKey ? '：弱实体的局部标识符，与所属执行共同标识' : attribute.key ? '：主键／标识符' : '' }}{{ attribute.multivalued ? '：多值属性（数组）' : '' }}</title>
          <ellipse :cx="attribute.x" :cy="attribute.y" :rx="attribute.rx" :ry="attribute.ry" fill="#ffffff" stroke="#475569" stroke-width="1.6" :stroke-dasharray="attribute.derived ? '6 4' : undefined" />
          <ellipse v-if="attribute.multivalued" :cx="attribute.x" :cy="attribute.y" :rx="attribute.rx - 5" :ry="attribute.ry - 5" fill="none" stroke="#475569" stroke-width="1.4" />
          <text :x="attribute.x" :y="attribute.y + 4" text-anchor="middle" font-family="monospace" font-size="12" :font-weight="attribute.key ? '700' : '400'" :text-decoration="attribute.key ? 'underline' : 'none'" :style="attribute.partialKey ? 'text-decoration-style: dashed' : undefined" fill="#1e293b">{{ attribute.name }}</text>
        </g>
      </svg>
    </div>
    <div class="chen-pagination">
      <p>{{ groups.length ? `共 ${groups.length} 个关联实体，当前显示第 ${page * pageSize + 1}–${Math.min((page + 1) * pageSize, groups.length)} 个` : '该实体没有已声明的联系，仍显示标识符与属性。' }}</p>
      <div v-if="groups.length > pageSize">
        <button type="button" :disabled="page === 0" @click="page--">上一组</button>
        <button type="button" :disabled="(page + 1) * pageSize >= groups.length" @click="page++">下一组</button>
      </div>
    </div>
    <ul class="chen-evidence" aria-label="当前联系的约束依据">
      <li v-for="relation in shown.flatMap(group => group.relations)" :key="relation.id"><strong>{{ relation.name }}</strong><span>{{ relation.detail }}</span></li>
    </ul>
    <details class="chen-legend" open>
      <summary>Chen 图形约定与阅读说明</summary>
      <div class="legend-grid">
        <div v-for="item in legend" :key="item.kind" class="legend-item">
          <svg viewBox="0 0 90 44" width="90" height="44" aria-hidden="true" fill="none" stroke="#475569" stroke-width="1.5">
            <template v-if="['entity', 'weak'].includes(item.kind)"><rect x="10" y="10" width="70" height="24" /><rect v-if="item.kind === 'weak'" x="14" y="14" width="62" height="16" /></template>
            <template v-else-if="['attribute', 'multi', 'derived', 'key'].includes(item.kind)"><ellipse cx="45" cy="22" rx="36" ry="14" :stroke-dasharray="item.kind === 'derived' ? '5 3' : undefined" /><ellipse v-if="item.kind === 'multi'" cx="45" cy="22" rx="31" ry="10" /><text v-if="item.kind === 'key'" x="45" y="26" text-anchor="middle" fill="#334155" stroke="none" font-size="13" font-weight="700" text-decoration="underline">id</text></template>
            <template v-else-if="['relationship', 'identifying'].includes(item.kind)"><polygon points="45,3 81,22 45,41 9,22" /><polygon v-if="item.kind === 'identifying'" points="45,8 73,22 45,36 17,22" /></template>
            <template v-else><line x1="8" y1="22" x2="82" y2="22" /><line v-if="item.kind === 'total'" x1="8" y1="27" x2="82" y2="27" /><text v-if="item.kind === 'cardinality'" x="13" y="15" fill="#334155" stroke="none" font-size="13">1</text><text v-if="item.kind === 'cardinality'" x="66" y="15" fill="#334155" stroke="none" font-size="13">N</text></template>
          </svg><span>{{ item.label }}</span>
        </div>
      </div>
      <p>双线仅在已核验的必需外键端使用；另一端不因存在外键而自动全参与。N 表示多，1 表示至多一个，可选参与由单线表达。弱实体由模型单独确认，局部标识符使用虚线下划线。多值属性来自数组字段；派生属性须有明确模型依据，当前不把已存储的计数字段当作派生属性。</p>
      <p>每个实体展示完整标识符及部分关键属性，完整字段和迁移来源可在下方查阅。图例说明符号，未声明的弱实体、多值或派生属性不会被自动添加。</p>
    </details>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { buildChenDiagram, parallelLines, relationGroups, type ChenRelation, type EntityDescription } from "../chen-model";
const props = defineProps<{ focus: string; entities: EntityDescription[]; options?: EntityDescription[]; relations: ChenRelation[] }>();
const emit = defineEmits<{ select: [name: string] }>();
const page = ref(0), pageSize = 2, zoom = ref(0.85);
const viewport = ref<HTMLDivElement>(), drawing = ref<SVGSVGElement>();
const current = computed(() => props.entities.find(e => e.name === props.focus)!);
const groups = computed(() => relationGroups(props.focus, props.relations));
const shown = computed(() => groups.value.slice(page.value * pageSize, (page.value + 1) * pageSize));
const diagram = computed(() => buildChenDiagram(current.value, props.entities, shown.value));
watch(() => props.focus, () => { page.value = 0; if (viewport.value) viewport.value.scrollLeft = 0; });
const diamond = (x: number, y: number, rx: number, ry: number) => `${x},${y - ry} ${x + rx},${y} ${x},${y + ry} ${x - rx},${y}`;
function wrapped(text: string, limit: number) {
  const parts: string[] = []; let part = "", width = 0;
  for (const char of text) { const size = char.charCodeAt(0) < 128 ? 0.55 : 1; if (width + size > limit) { parts.push(part); part = ""; width = 0; } part += char; width += size; }
  if (part) parts.push(part); return parts;
}
function fit() { zoom.value = Math.max(0.1, Math.min(1, (viewport.value?.clientWidth || 1000) / diagram.value.width)); }
function download() {
  if (!drawing.value) return;
  const clone = drawing.value.cloneNode(true) as SVGSVGElement;
  clone.setAttribute("width", String(diagram.value.width)); clone.setAttribute("height", String(diagram.value.height));
  const url = URL.createObjectURL(new Blob([new XMLSerializer().serializeToString(clone)], { type: "image/svg+xml;charset=utf-8" }));
  const anchor = document.createElement("a"); anchor.href = url; anchor.download = `CampusOS-Chen-${props.focus}.svg`; anchor.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
const legend = [
  { kind: "entity", label: "实体：矩形" }, { kind: "weak", label: "弱实体：双矩形" },
  { kind: "attribute", label: "属性：椭圆" }, { kind: "multi", label: "多值属性：双椭圆" },
  { kind: "derived", label: "派生属性：虚线椭圆" }, { kind: "key", label: "标识符：加粗、下划线" },
  { kind: "relationship", label: "联系：菱形" }, { kind: "identifying", label: "标识性联系：双菱形" },
  { kind: "total", label: "全参与：双线" }, { kind: "partial", label: "部分参与：单线" },
  { kind: "cardinality", label: "基数：1 / M / N" },
];
</script>

<style scoped>
.chen-view { min-width: 0; border: 1px solid #dbe4ee; border-radius: 8px; background: white; }
.chen-controls, .chen-buttons, .chen-examples, .chen-pagination { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; }
.chen-controls { padding: 16px; justify-content: space-between; }
.chen-selector { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; max-width: 100%; }
.chen-selector select { max-width: 100%; width: 360px; padding: 7px; border: 1px solid #cbd5e1; border-radius: 4px; background: white; }
.chen-view button { padding: 6px 10px; border: 1px solid #cbd5e1; border-radius: 4px; background: white; color: #334155; cursor: pointer; }
.chen-view button:hover { background: #eff6ff; }
.chen-view button:disabled { opacity: 0.5; cursor: default; }
.chen-view button:focus-visible, .chen-selector select:focus-visible, .chen-viewport:focus-visible { outline: 2px solid #2563eb; outline-offset: 2px; }
.chen-buttons output { min-width: 44px; text-align: center; }
.chen-examples { padding: 0 16px; font-size: 13px; }
.chen-help { padding: 12px 16px; margin: 0; color: #64748b; font-size: 13px; line-height: 1.6; }
.chen-viewport { max-width: 100%; overflow: auto; max-height: 760px; border-block: 1px solid #e2e8f0; }
.chen-viewport svg { display: block; max-width: none; }
.chen-entity { cursor: pointer; outline: none; }
.chen-entity:focus-visible rect:first-of-type { stroke: #2563eb; stroke-width: 4; }
.chen-pagination { padding: 8px 16px; justify-content: space-between; color: #64748b; font-size: 13px; }
.chen-pagination > div { display: flex; gap: 8px; }
.chen-evidence { margin: 0; padding: 0 16px 16px 34px; color: #64748b; font-size: 12px; line-height: 1.8; overflow-wrap: anywhere; }
.chen-evidence strong { margin-right: 8px; color: #334155; }
.chen-legend { border-top: 1px solid #e2e8f0; padding: 16px; }
.chen-legend summary { cursor: pointer; font-weight: 600; color: #334155; }
.legend-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(195px, 1fr)); gap: 8px; margin-top: 16px; }
.legend-item { display: flex; align-items: center; gap: 5px; font-size: 12px; color: #475569; }
.chen-legend p { margin: 12px 0 0; color: #64748b; font-size: 12px; line-height: 1.8; }
@media (max-width: 600px) { .chen-controls { align-items: stretch; } .chen-selector { display: block; width: 100%; } .chen-selector label { display: block; margin-bottom: 8px; } .chen-selector select { width: 100%; } }
</style>
