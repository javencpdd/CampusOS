// Test driver only. Lifecycle/configuration use actual authenticated HTTP;
// typed calls cross a secret-protected test control into the SAME Server's
// managed dispatcher. There is no production arbitrary-provider HTTP route.
import assert from "node:assert/strict";
import { writeFile } from "node:fs/promises";
let state;
export async function ecosystemPhase(phase, c) {
  const { cfg, ap, up, bp, api, identity, ok, check } = c;
  const path = (domain) => `${domain === "admin" ? "/admin" : ""}/plugin-v5`;
  let sequence = 0;
  const detail = async (page, domain, version) =>
    ok(
      await api(
        page,
        domain,
        "GET",
        `${path(domain)}/campusos/chat-echo/${version}`,
      ),
    );
  const command = async (
    page,
    domain,
    version,
    kind,
    extra = {},
    expectedStatus = 200,
  ) => {
    const d = await detail(page, domain, version);
    const revision = ["grant", "revoke"].includes(kind)
      ? d.grant?.revision
      : kind === "configure"
        ? d.configuration?.revision
        : undefined;
    const r = await api(
      page,
      domain,
      "POST",
      `${path(domain)}/campusos/chat-echo/commands`,
      {
        kind,
        version,
        idempotency_key: `ecosystem-${phase}-${Date.now()}-${++sequence}`,
        expected_generation: d.installation?.generation || "0",
        ...(revision ? { expected_revision: revision } : {}),
        ...extra,
      },
    );
    assert.equal(
      r.status,
      expectedStatus,
      `${kind} ${domain} ${version}: ${r.text}`,
    );
    return r.data?.data;
  };
  const wait = async (page, domain, version, observed, installed = true) => {
    const until = Date.now() + 45000;
    for (;;) {
      const d = await detail(page, domain, version);
      if (
        d.installation?.observed === observed &&
        d.installation.installed === installed &&
        (observed !== "ready" ||
          (d.installation.version === version &&
            d.installation.active_generation !== "0"))
      )
        return d;
      assert.ok(
        Date.now() < until,
        `headless readiness ${JSON.stringify(d.installation)}`,
      );
      await new Promise((r) => setTimeout(r, 200));
    }
  };
  const configure = (page, domain, version, prefix) =>
    command(page, domain, version, "configure", {
      configuration_part: "values",
      values: { prefix },
    });
  const contract = async (page, action, extra = {}, status = 200) => {
    const token = page ? (await identity(page, "user")).token : "";
    const r = await fetch(cfg.control + "/contract", {
      method: "POST",
      headers: {
        Authorization: "Bearer " + cfg.controlKey,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        Action: action,
        Token: token,
        Version: state.version,
        Prompt: 'hello "世界"\n',
        ContextClass: "none",
        DeadlineMS: 3000,
        ...extra,
      }),
    });
    const wire = await r.text();
    assert.equal(r.status, status, `typed ${action}: ${wire}`);
    if (status !== 200) {
      assert.equal(wire, "");
      return;
    }
    return JSON.parse(wire);
  };
  const call = async (page, prefix, extra = {}) => {
    // Observe commits durable readiness before the coordinator publishes its
    // managed handle. Poll a side-effect-free selection, never replay prompt
    // execution or a denied lifecycle mutation.
    const until = Date.now() + 15000;
    for (;;) {
      const r = await fetch(cfg.control + "/contract", {
        method: "POST",
        headers: {
          Authorization: "Bearer " + cfg.controlKey,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          Action: "ready",
          Token: (await identity(page, "user")).token,
          Version: state.version,
        }),
      });
      assert.ok(
        [204, 403].includes(r.status),
        "typed publication readiness probe",
      );
      if (r.status === 204) break;
      assert.ok(
        Date.now() < until,
        "current managed contribution publication deadline",
      );
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    const out = await contract(page, "call", extra);
    assert.equal(out.text, prefix + 'hello "世界"\n');
    assert.equal(out.request_id, "ecosystem08a");
    assert.ok(out.instance_generation > 0);
    return out;
  };
  const approve = async (version) => {
    const d = await detail(ap, "admin", version);
    return command(ap, "admin", version, "grant", {
      approved: true,
      consumes: d.release.manifest.consumes,
      host_resources: d.release.manifest.host_resources,
      reason: "08a independent author release review",
    });
  };
  if (phase === "coexist") {
    state = { version: "1.1.0", leaves: [] };
    const leaf = (s) => {
      state.leaves.push(s);
      check(s);
    };
    const d = await detail(ap, "admin", state.version);
    assert.equal(d.release.manifest.runtime, "container");
    assert.deepEqual(d.release.manifest.ui || [], []);
    assert.deepEqual(d.release.manifest.consumes, []);
    await command(ap, "admin", state.version, "install");
    await command(ap, "admin", state.version, "enable", {}, 403);
    await approve(state.version);
    await configure(ap, "admin", state.version, "system: ");
    await command(ap, "admin", state.version, "enable");
    await wait(ap, "admin", state.version, "ready");
    for (const [page, prefix] of [
      [up, "alice: "],
      [bp, "bob: "],
    ]) {
      await command(page, "user", state.version, "install");
      await configure(page, "user", state.version, prefix);
      await command(page, "user", state.version, "enable");
      await wait(page, "user", state.version, "ready");
      await call(page, prefix);
    }
    const a = await contract(up, "remember", { Handle: "alice-original" });
    const b = await contract(bp, "remember", { Handle: "bob-original" });
    assert.notEqual(a.instance_generation, b.instance_generation);
    await contract(bp, "cached", { Handle: "alice-original" }, 403);
    for (const extra of [
      { ContextClass: "public" },
      { DeadlineMS: 0 },
      { DeadlineMS: 300001 },
    ])
      await contract(up, "call", extra, 403);
    await contract(null, "feature-off", {}, 204);
    await contract(up, "cached", { Handle: "alice-original" }, 403);
    await contract(null, "feature-on", {}, 204);
    await call(up, "alice: ");
    leaf(
      "same read-only Server: signed headless author package, separate approval, two current user sessions and isolated typed responses; cross-owner/context/deadline/disabled Feature fail closed",
    );
    await configure(up, "user", state.version, "changed: ");
    await contract(up, "cached", { Handle: "alice-original" }, 403);
    await wait(up, "user", state.version, "ready");
    await call(up, "changed: ");
    await call(bp, "bob: ");
    for (let n = 0; n < 2; n++) {
      await contract(up, "remember", { Handle: "before-reload" });
      await command(up, "user", state.version, "disable");
      await wait(up, "user", state.version, "stopped");
      await command(up, "user", state.version, "enable");
      await wait(up, "user", state.version, "ready");
      await contract(up, "cached", { Handle: "before-reload" }, 403);
      await call(up, "changed: ");
      await call(bp, "bob: ");
    }
    leaf(
      "same read-only Server: current configuration generation and two real disable/enable reloads fence old bindings while the second owner stays usable",
    );
    await contract(up, "remember", { Handle: "before-upgrade" });
    const target = "1.1.1";
    const td = await detail(ap, "admin", target);
    assert.ok(!td.grant?.approved);
    await command(ap, "admin", target, "upgrade", {}, 403);
    await approve(target);
    await configure(ap, "admin", target, "system-v2: ");
    await command(ap, "admin", target, "upgrade");
    await wait(ap, "admin", target, "ready");
    await contract(up, "cached", { Handle: "before-upgrade" }, 403);
    state.version = target;
    for (const [page, prefix] of [
      [up, "alice-v2: "],
      [bp, "bob-v2: "],
    ]) {
      await configure(page, "user", target, prefix);
      await command(page, "user", target, "upgrade");
      await wait(page, "user", target, "ready");
      await call(page, prefix);
    }
    leaf(
      "same read-only Server: independent signed 1.1.1 release requires new approval and performs ready cutover for global and both owner instances without host restart",
    );
    // Leave these real workers running while the PDF journey revokes/expires
    // its separate Grant and uninstalls the user's PDF installation.
    await writeFile(
      cfg.output + "/headless-coexistence.json",
      JSON.stringify(
        {
          status: "passed",
          version: state.version,
          leaves: state.leaves,
          headless_ui: false,
          owners: 2,
        },
        null,
        2,
      ) + "\n",
    );
    return;
  }
  assert.equal(phase, "exit");
  await call(up, "alice-v2: ");
  await call(bp, "bob-v2: ");
  check(
    "PDF revocation, expiry and owner uninstall preserve both headless typed instances on the same read-only Server",
  );
  await contract(up, "remember", { Handle: "before-revoke" });
  await command(ap, "admin", state.version, "revoke");
  await contract(up, "cached", { Handle: "before-revoke" }, 403);
  await contract(bp, "call", {}, 403);
  for (const page of [up, bp]) {
    await command(page, "user", state.version, "uninstall", {
      purge_data: true,
    });
    await wait(page, "user", state.version, "stopped", false);
  }
  await command(ap, "admin", state.version, "uninstall", { purge_data: true });
  await wait(ap, "admin", state.version, "stopped", false);
  // Old-version installations share the same per-owner current record. Both
  // signed releases must become collectable only after all owners uninstall.
  for (const version of ["1.1.0", "1.1.1"]) {
    const op = await command(ap, "admin", version, "purge_release");
    const until = Date.now() + 15000;
    for (;;) {
      const view = ok(
        await api(ap, "admin", "GET", `/admin/plugin-v5/operations/${op.id}`),
      );
      if (view.status === "done") break;
      assert.ok(Date.now() < until, `release GC ${JSON.stringify(view)}`);
      await new Promise((r) => setTimeout(r, 200));
    }
    assert.equal(
      (await detail(ap, "admin", version)).release.storage,
      "purged",
    );
  }
  check(
    "headless current revocation closes both owners immediately; explicit owner/global uninstall and both immutable package GC complete through actual HTTP operations",
  );
  await writeFile(
    cfg.output + "/headless-exit.json",
    JSON.stringify(
      {
        status: "passed",
        owners: 2,
        releases: ["1.1.0", "1.1.1"],
        same_server: true,
        typed_consumer: "compiled test consumer only",
        upgraded: true,
        reloads: 2,
        revoked: true,
        uninstalled: true,
        purged: true,
        leaves: state.leaves,
      },
      null,
      2,
    ) + "\n",
  );
}
