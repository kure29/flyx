/** Clients a subscription can be imported into. format is the link's
 * ?format= value; importURL opens the app's own import (null: copy only).
 * With routing rules, clients with configImportURL set get a full
 * configuration instead of a node list, imported through it. */
export interface SubClient {
  key: string;
  label: string;
  format: string;
  importURL: ((url: string, name: string) => string) | null;
  configImportURL?: ((url: string, name: string) => string) | null;
}

const enc = encodeURIComponent;

export const SUB_CLIENTS: SubClient[] = [
  {
    key: "clash",
    label: "mihomo",
    format: "clash",
    importURL: (url, name) =>
      `clash://install-config?url=${enc(url)}&name=${enc(name)}`,
  },
  {
    key: "singbox",
    label: "sing-box",
    format: "singbox",
    importURL: (url, name) =>
      `sing-box://import-remote-profile?url=${enc(url)}#${enc(name)}`,
  },
  {
    key: "shadowrocket",
    label: "Shadowrocket",
    format: "shadowrocket",
    importURL: (url, name) =>
      `shadowrocket://add/sub://${btoa(url)}?remark=${enc(name)}`,
    configImportURL: (url) => `shadowrocket://config/add/${url}`,
  },
  {
    key: "loon",
    label: "Loon",
    format: "loon",
    importURL: (url) => `loon://import?nodelist=${enc(url)}`,
    configImportURL: null,
  },
  {
    key: "qx",
    label: "Quantumult X",
    format: "qx",
    importURL: (url, name) =>
      `quantumult-x:///add-resource?remote-resource=${enc(
        JSON.stringify({ server_remote: [`${url}, tag=${name}`] }),
      )}`,
    configImportURL: null,
  },
  {
    key: "surge",
    label: "Surge",
    format: "surge",
    importURL: (url) => `surge:///install-config?url=${enc(url)}`,
  },
  {
    key: "stash",
    label: "Stash",
    format: "stash",
    importURL: (url) => `stash://install-config?url=${enc(url)}`,
  },
  { key: "base64", label: "通用", format: "base64", importURL: null },
];
