// @vitest-environment jsdom
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import ChenDiagram from "../src/modules/architecture/components/ChenDiagram.vue";
import { buildChenRelations, buildChenDiagram, entityAttributes, relationGroups, schemaFacts, weakEntities } from "../src/modules/architecture/chen-model";

const entities = Object.keys(schemaFacts).map(name => ({ name, title: name, fields: schemaFacts[name].columns }));
const relations = buildChenRelations(entities, []);
const entity = (name: string) => entities.find(e => e.name === name)!;

describe("Chen notation from reviewed schema facts", () => {
  it("preserves every entity identifier, including composite and system keys", () => {
    expect(entities).toHaveLength(132);
    for (const e of entities) {
      const attributes = entityAttributes(e, 3);
      expect(attributes.filter(a => a.key).map(a => a.name)).toEqual(schemaFacts[e.name].primaryKey);
    }
    expect(schemaFacts.schema_migrations.primaryKey).toEqual(["version"]);
    expect(schemaFacts.schema_migration_locks.primaryKey).toEqual(["id"]);
  });
  it("corrects handwritten one-to-many using uniqueness, without inferring total parent participation", () => {
    const result = buildChenRelations(entities, [{ id: "legacy", source: "ai_model_profiles", target: "ai_model_budgets", sourceCardinality: "1", targetCardinality: "N", label: "old" }]);
    const budget = result.filter(r => r.source === "ai_model_profiles" && r.target === "ai_model_budgets");
    expect(budget).toHaveLength(1);
    expect(budget[0]).toMatchObject({ sourceCardinality: "1", targetCardinality: "1", sourceTotal: false, targetTotal: true, basis: "foreign-key" });
    expect(new Set(relations.map(r => r.id)).size).toBe(relations.length);
  });
  it("keeps optional and logical participation single, and only declares reviewed weak entities", () => {
    const parent = relations.find(r => r.source === "posts" && r.target === "posts")!;
    expect(parent.targetTotal).toBe(false);
    const result = buildChenRelations(entities, [{ id: "audit", source: "admin_accounts", target: "integration_principals", sourceCardinality: "1", targetCardinality: "0..N", label: "logical audit" }]);
    expect(result.find(r => r.id === "logical:audit")).toMatchObject({ basis: "logical", sourceTotal: false, targetTotal: false, identifying: false });
    expect(weakEntities.user_roles).toBeUndefined();
    expect(relations.find(r => r.target === "ai_model_run_events")).toMatchObject({ identifying: true, targetTotal: true });
  });
  it("does not turn persisted counters or JSON objects into derived or multivalued attributes", () => {
    const attributes = entityAttributes(entity("threads"), 100);
    expect(attributes.find(a => a.name === "tags")).toMatchObject({ multivalued: true, derived: false });
    expect(attributes.find(a => a.name === "reply_count")).toMatchObject({ multivalued: false, derived: false });
    expect(attributes.find(a => a.name === "metadata")).toMatchObject({ multivalued: false });
  });
  it("draws double rectangles, double diamonds, key ovals and arrowless participation lines", () => {
    const wrapper = mount(ChenDiagram, { props: { focus: "ai_model_runs", entities, relations } });
    const weak = wrapper.get('[data-entity="ai_model_run_events"]');
    expect(weak.findAll("rect")).toHaveLength(2);
    expect(wrapper.get('[data-identifying="true"]').findAll("polygon")).toHaveLength(2);
    expect(wrapper.get('[data-attribute="ai_model_run_events:ordinal"] text').attributes("text-decoration")).toBe("underline");
    expect(wrapper.findAll('[data-participation="total"]').every(g => g.findAll("line").length === 2)).toBe(true);
    expect(wrapper.find("marker").exists()).toBe(false);
    expect(wrapper.find('[marker-end], [marker-start]').exists()).toBe(false);
    wrapper.unmount();
  });
  it("draws double ovals and lets a keyboard selection inspect another entity", async () => {
    const wrapper = mount(ChenDiagram, { props: { focus: "threads", entities, relations } });
    expect(wrapper.get('[data-attribute="threads:tags"]').findAll("ellipse")).toHaveLength(2);
    await wrapper.get('[data-entity="categories"]').trigger("keydown", { key: "Enter" });
    expect(wrapper.emitted("select")?.[0]).toEqual(["categories"]);
    wrapper.unmount();
  });
  it("renders self relationships once and keeps all shapes within the drawing", () => {
    for (const e of entities) {
      const groups = relationGroups(e.name, relations);
      for (let offset = 0; offset < Math.max(1, groups.length); offset += 2) {
        const diagram = buildChenDiagram(e, entities, groups.slice(offset, offset + 2));
        expect(new Set(diagram.entities.map(n => n.id)).size).toBe(diagram.entities.length);
        for (const a of diagram.attributes) {
          expect(a.y - a.ry).toBeGreaterThanOrEqual(0);
          expect(a.y + a.ry).toBeLessThanOrEqual(diagram.height);
        }
        for (const line of diagram.lines) for (const point of [line.start, line.end]) {
          expect(Number.isFinite(point.x) && Number.isFinite(point.y)).toBe(true);
        }
      }
    }
  });
});
