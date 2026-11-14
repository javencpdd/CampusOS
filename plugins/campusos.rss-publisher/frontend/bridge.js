import { identity } from "./identity.js";
const version = "campusos.bridge/v1";
export class Bridge {
  pending = new Map();
  sequence = 0;
  port = null;
  constructor(host) {
    this.host = host;
  }
  connect() {
    return new Promise((resolve, reject) => {
      const fail = setTimeout(() => {
        window.removeEventListener("message", listen);
        reject(new Error("连接超时，请从 CampusOS 插件中心重新打开。"));
      }, 10000);
      const listen = (event) => {
        const data = event.data;
        if (
          event.origin !== this.host ||
          event.source !== parent ||
          !event.ports[0] ||
          data?.type !== "campusos.bridge.connect" ||
          data.version !== version ||
          Object.entries(identity).some(([k, v]) => data[k] !== v)
        )
          return;
        clearTimeout(fail);
        window.removeEventListener("message", listen);
        this.port = event.ports[0];
        this.port.onmessage = ({ data }) => {
          if (data?.version !== version) return;
          const request = this.pending.get(data.request_id);
          if (!request) return;
          this.pending.delete(data.request_id);
          clearTimeout(request.timer);
          if (data.error) request.reject(new Error(data.error.message));
          else request.resolve(data.result);
        };
        this.port.start();
        this.port.postMessage({
          type: "campusos.bridge.handshake",
          version,
          challenge: data.challenge,
        });
        resolve();
      };
      window.addEventListener("message", listen);
      parent.postMessage(
        { type: "campusos.bridge.ready", version, ...identity },
        this.host,
      );
    });
  }
  request(method, params) {
    if (!this.port) return Promise.reject(new Error("插件尚未连接。"));
    const request_id = "rss-" + ++this.sequence;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(
        () => {
          this.pending.delete(request_id);
          reject(new Error("操作超时，请核对结果后重试。"));
        },
        method === "editor.article.import" ? 185000 : 35000,
      );
      this.pending.set(request_id, { resolve, reject, timer });
      this.port.postMessage({ version, request_id, method, params });
    });
  }
  close() {
    this.port?.close();
    for (const r of this.pending.values()) {
      clearTimeout(r.timer);
      r.reject(new Error("连接已关闭。"));
    }
    this.pending.clear();
  }
}
