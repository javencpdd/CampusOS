import snapshot from "./schema.generated.json";

export interface EntityDescription { name: string; title: string; fields: string[] }
export interface LogicalRelation {
  id: string; source: string; target: string;
  sourceCardinality: string; targetCardinality: string; label: string;
}
interface ForeignKey { name: string; columns: string[]; parent: string; parentColumns: string[]; required: boolean; unique: boolean }
interface SchemaEntity { primaryKey: string[]; columns: string[]; multivalued: string[]; foreignKeys: ForeignKey[] }
export const schemaFacts: Record<string, SchemaEntity> = snapshot.tables;

// Weakness is a conceptual decision, not an automatic consequence of having a
// foreign key. An event ordinal is meaningful only within its owning run.
export const weakEntities: Record<string, { owner: string; localKey: string[] }> = {
  ai_model_run_events: { owner: "ai_model_runs", localKey: ["ordinal"] },
};
export interface ChenAttribute { name: string; key: boolean; partialKey: boolean; multivalued: boolean; derived: boolean }
export interface ChenRelation {
  id: string; source: string; target: string; name: string; detail: string;
  sourceCardinality: "1" | "N"; targetCardinality: "1" | "N";
  sourceTotal: boolean; targetTotal: boolean; identifying: boolean; basis: "foreign-key" | "logical";
}
const relationshipNames: Record<string, string> = {
  "users:threads:author_id": "发表", "users:posts:author_id": "撰写",
  "categories:threads:category_id": "收录", "threads:posts:thread_id": "包含",
  "posts:posts:parent_id": "回复", "categories:categories:parent_id": "隶属",
  "ai_model_runs:ai_model_run_events:run_id": "记录",
  "admin_accounts:admin_credentials:account_id": "认证",
  "users:accounts:user_id": "拥有",
};
function relationshipName(parent: string, child: string, columns: string[]) {
  const explicit = relationshipNames[`${parent}:${child}:${columns.join(",")}`];
  if (explicit) return explicit;
  const names = columns.join(",");
  if (/owner|author/.test(names)) return "拥有";
  if (/role/.test(names)) return "绑定";
  if (/profile/.test(names)) return "采用";
  if (/collection/.test(names)) return "收录";
  if (/version/.test(names)) return "引用版本";
  if (/parent/.test(names)) return "隶属";
  return "关联";
}
export function buildChenRelations(entities: EntityDescription[], logical: LogicalRelation[]): ChenRelation[] {
  const names = new Set(entities.map(e => e.name));
  const result: ChenRelation[] = [];
  const physicalPairs = new Set<string>();
  for (const entity of entities) {
    for (const fk of schemaFacts[entity.name]?.foreignKeys || []) {
      if (!names.has(fk.parent)) continue;
      const identifying = weakEntities[entity.name]?.owner === fk.parent && fk.columns.every(c => schemaFacts[entity.name].primaryKey.includes(c));
      result.push({ id: `fk:${entity.name}:${fk.name}`, source: fk.parent, target: entity.name,
        name: relationshipName(fk.parent, entity.name, fk.columns),
        detail: `${entity.name}.(${fk.columns.join(", ")}) → ${fk.parent}.(${fk.parentColumns.join(", ")})；${fk.name}`,
        sourceCardinality: "1", targetCardinality: fk.unique ? "1" : "N",
        sourceTotal: false, targetTotal: fk.required, identifying, basis: "foreign-key" });
      physicalPairs.add(`${fk.parent}:${entity.name}`);
    }
  }
  // Existing reviewed logical references remain available, explicitly labelled.
  // A schema FK replaces old handwritten cardinality for the same table pair.
  for (const relation of logical) {
    if (physicalPairs.has(`${relation.source}:${relation.target}`)) continue;
    if (!names.has(relation.source) || !names.has(relation.target)) continue;
    result.push({ id: `logical:${relation.id}`, source: relation.source, target: relation.target,
      name: "关联", detail: relation.label,
      sourceCardinality: /[MN]/i.test(relation.sourceCardinality) ? "N" : "1",
      targetCardinality: /[MN]/i.test(relation.targetCardinality) ? "N" : "1",
      sourceTotal: false, targetTotal: false, identifying: false, basis: "logical" });
  }
  return result;
}
export function entityAttributes(entity: EntityDescription, limit = 5): ChenAttribute[] {
  const facts = schemaFacts[entity.name];
  if (!facts) throw new Error(`Missing reviewed schema facts for ${entity.name}`);
  const names = [...new Set([...facts.primaryKey, ...facts.multivalued, ...entity.fields.filter(f => facts.columns.includes(f))])];
  return names.slice(0, Math.max(limit, facts.primaryKey.length)).map(name => ({ name,
    key: facts.primaryKey.includes(name), partialKey: weakEntities[entity.name]?.localKey.includes(name) || false,
    multivalued: facts.multivalued.includes(name), derived: false }));
}

export interface Point { x: number; y: number }
export interface DiagramEntity { id: string; table: EntityDescription; x: number; y: number; width: number; height: number; weak: boolean }
export interface DiagramAttribute extends ChenAttribute { id: string; owner: string; x: number; y: number; rx: number; ry: number; start: Point; end: Point }
export interface DiagramRelationship { relation: ChenRelation; x: number; y: number }
export interface DiagramLine { id: string; start: Point; end: Point; double: boolean; cardinality?: string; label?: Point }
export interface Diagram { width: number; height: number; entities: DiagramEntity[]; attributes: DiagramAttribute[]; relationships: DiagramRelationship[]; lines: DiagramLine[] }
export interface RelationGroup { name: string; relations: ChenRelation[] }
export function relationGroups(focus: string, relations: ChenRelation[]): RelationGroup[] {
  const grouped = new Map<string, ChenRelation[]>();
  for (const relation of relations) {
    if (relation.source !== focus && relation.target !== focus) continue;
    const other = relation.source === focus ? relation.target : relation.source;
    grouped.set(other, [...(grouped.get(other) || []), relation]);
  }
  const priority = focus === "users" ? ["threads", "posts", "accounts"] : [];
  return [...grouped].map(([name, relations]) => ({ name, relations })).sort((a, b) => {
    const rank = (name: string) => priority.includes(name) ? priority.indexOf(name) : 99;
    return rank(a.name) - rank(b.name) || a.name.localeCompare(b.name);
  });
}
function boundary(entity: DiagramEntity, toward: Point): Point {
  const dx = toward.x - entity.x, dy = toward.y - entity.y;
  const ratio = Math.min(dx ? entity.width / 2 / Math.abs(dx) : Infinity, dy ? entity.height / 2 / Math.abs(dy) : Infinity);
  return { x: entity.x + dx * ratio, y: entity.y + dy * ratio };
}
export function parallelLines(start: Point, end: Point, double: boolean): { start: Point; end: Point }[] {
  if (!double) return [{ start, end }];
  const length = Math.hypot(end.x - start.x, end.y - start.y) || 1;
  const dx = -(end.y - start.y) / length * 2.5, dy = (end.x - start.x) / length * 2.5;
  return [-1, 1].map(sign => ({ start: { x: start.x + dx * sign, y: start.y + dy * sign }, end: { x: end.x + dx * sign, y: end.y + dy * sign } }));
}
export function buildChenDiagram(focus: EntityDescription, descriptions: EntityDescription[], groups: RelationGroup[]): Diagram {
  const focusAttributes = entityAttributes(focus);
  const sizes = groups.map(g => Math.max(240, entityAttributes(descriptions.find(e => e.name === g.name)!, 3).length * 64 + 50, g.relations.length * 130 + 50));
  const height = Math.max(560, sizes.reduce((sum, n) => sum + n, 0) + 100);
  const result: Diagram = { width: 1660, height, entities: [], attributes: [], relationships: [], lines: [] };
  const center: DiagramEntity = { id: focus.name, table: focus, x: 370, y: height / 2, width: 260, height: 96, weak: !!weakEntities[focus.name] };
  result.entities.push(center);
  function attributes(entity: DiagramEntity, fields: ChenAttribute[], side: "left" | "right") {
    fields.forEach((field, index) => {
      const x = side === "left" ? 112 : 1490, y = entity.y + (index - (fields.length - 1) / 2) * 64;
      const rx = 106, ry = 25;
      const start = boundary(entity, { x, y });
      const dx = start.x - x, dy = start.y - y;
      const ratio = 1 / Math.sqrt(dx * dx / rx ** 2 + dy * dy / ry ** 2);
      result.attributes.push({ ...field, id: `${entity.id}:${field.name}`, owner: entity.id, x, y, rx, ry, start, end: { x: x + dx * ratio, y: y + dy * ratio } });
    });
  }
  attributes(center, focusAttributes, "left");
  let top = (height - sizes.reduce((sum, n) => sum + n, 0)) / 2;
  groups.forEach((group, index) => {
    const y = top + sizes[index] / 2; top += sizes[index];
    const self = group.name === focus.name;
    const table = descriptions.find(e => e.name === group.name)!;
    const other: DiagramEntity = self ? center : { id: group.name, table, x: 1160, y, width: 260, height: 96, weak: !!weakEntities[group.name] };
    if (!self) { result.entities.push(other); attributes(other, entityAttributes(table, 3), "right"); }
    group.relations.forEach((relation, n) => {
      const ry = y + (n - (group.relations.length - 1) / 2) * 130;
      const diamond = { x: 790, y: ry };
      result.relationships.push({ relation, ...diamond });
      const normal = relation.source === focus.name;
      const leftVertex = { x: diamond.x - 86, y: diamond.y };
      const rightVertex = self ? { x: diamond.x, y: diamond.y + 48 } : { x: diamond.x + 86, y: diamond.y };
      const start = self ? { x: center.x + center.width / 2, y: center.y - 24 } : boundary(center, leftVertex);
      const end = self ? { x: center.x + center.width / 2, y: center.y + 24 } : boundary(other, rightVertex);
      const line = (id: string, from: Point, to: Point, total: boolean, cardinality: string): DiagramLine => ({ id, start: from, end: to, double: total, cardinality, label: { x: from.x + (to.x - from.x) * 0.16, y: from.y + (to.y - from.y) * 0.16 - 12 } });
      result.lines.push(line(`${relation.id}:left`, start, leftVertex, normal ? relation.sourceTotal : relation.targetTotal, normal ? relation.sourceCardinality : relation.targetCardinality));
      result.lines.push(line(`${relation.id}:right`, end, rightVertex, normal ? relation.targetTotal : relation.sourceTotal, normal ? relation.targetCardinality : relation.sourceCardinality));
    });
  });
  return result;
}
