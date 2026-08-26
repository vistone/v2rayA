<template>
  <div class="pool-panel">
    <div class="level">
      <div class="level-left">
        <b-button type="is-primary" size="is-small" icon-left="plus" @click="openEdit(null)">
          {{ $t("pool.create") }}
        </b-button>
        <b-button size="is-small" icon-left="refresh" @click="load">{{ $t("pool.refresh") }}</b-button>
      </div>
      <div class="level-right">
        <span class="is-size-7 has-text-grey">{{ $t("pool.refreshHint") }}</span>
      </div>
    </div>

    <div v-if="!pools.length" class="pool-empty">
      <div class="pool-empty__icon">🌐</div>
      <div class="pool-empty__title">{{ $t("pool.noPools") }}</div>
      <div class="pool-empty__desc">{{ $t("pool.noPoolsDesc") }}</div>
      <b-button type="is-primary" icon-left="plus" @click="openEdit(null)">{{ $t("pool.create") }}</b-button>
    </div>

    <div v-for="p in pools" :key="p.name" class="box pool-card">
      <div class="level is-mobile pool-card__header">
        <div class="level-left pool-card__title">
          <span class="has-text-weight-medium is-size-5">{{ p.name }}</span>
          <b-tag v-if="p.outbound && p.outbound !== p.name" size="is-small" class="ml-2">{{ p.outbound }}</b-tag>
          <b-tag size="is-small" class="ml-2 is-light">{{ $t("pool.trafficGatePct") }} {{ p.settings.trafficGatePct }}%</b-tag>
          <b-tag size="is-small" class="ml-1 is-light">{{ p.settings.pollInterval }}</b-tag>
        </div>
        <div class="level-right">
          <b-button size="is-small" icon-left="pencil" @click="openEdit(p)">{{ $t("pool.edit") }}</b-button>
          <b-button size="is-small" type="is-danger" outlined icon-left="delete" @click="remove(p)">
            {{ $t("pool.delete") }}
          </b-button>
        </div>
      </div>
      <table class="table is-fullwidth is-narrow is-hoverable">
        <thead>
          <tr>
            <th>{{ $t("pool.member") }}</th>
            <th style="width: 12%">{{ $t("pool.latency") }}</th>
            <th style="width: 24%">{{ $t("pool.traffic") }}</th>
            <th style="width: 12%">{{ $t("pool.gate") }}</th>
            <th style="width: 12%">{{ $t("pool.status") }}</th>
            <th style="width: 18%">{{ $t("pool.actions") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="m in p.members" :key="memberKey(m.which)">
            <td>{{ memberName(m.which) }}</td>
            <td>{{ latencyText(m) }}</td>
            <td>
              <template v-if="m.status && m.status.traffic">
                <progress
                  class="progress is-small mb-1"
                  :class="progressClass(m.status.traffic.usedPct, m.status.traffic.stopPct)"
                  :value="m.status.traffic.usedPct"
                  max="100"
                ></progress>
                <span class="is-size-7 has-text-grey">
                  {{ pctText(m.status.traffic.usedPct) }} ·
                  {{ bytesText(m.status.traffic.usedBytes) }} / {{ bytesText(m.status.traffic.quotaBytes) }}
                  <b-tag v-if="m.status.traffic.tripped" type="is-danger" size="is-small">TRIPPED</b-tag>
                </span>
              </template>
              <span v-else class="has-text-grey">-</span>
            </td>
            <td>
              <b-tag :type="m.gate === 'active' ? 'is-success' : 'is-danger'" size="is-small">
                {{ m.gate === "active" ? $t("pool.inPool") : $t("pool.excluded") }}
              </b-tag>
            </td>
            <td>
              <b-tag v-if="m.reachable" type="is-success" size="is-small">{{ $t("pool.online") }}</b-tag>
              <b-tag v-else-if="m.err" type="is-danger" size="is-small" :title="m.err">
                {{ $t("pool.offline") }}
              </b-tag>
              <b-tag v-else size="is-small">{{ $t("pool.unknown") }}</b-tag>
            </td>
            <td>
              <b-button
                size="is-small"
                :loading="busy[busyKey(p.name, m.which, 'trip')]"
                @click="control(p, m, 'trip')"
              >{{ $t("pool.trip") }}</b-button>
              <b-button
                size="is-small"
                :loading="busy[busyKey(p.name, m.which, 'resume')]"
                @click="control(p, m, 'resume')"
              >{{ $t("pool.resume") }}</b-button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 新建/编辑 -->
    <b-modal :active.sync="editActive" has-modal-card trap-focus :can-cancel="!saving">
      <div class="modal-card" style="width: 780px">
        <header class="modal-card-head">
          <p class="modal-card-title">{{ editing ? $t("pool.edit") : $t("pool.create") }}</p>
        </header>
        <section class="modal-card-body pool-edit-body">
          <b-field :label="$t('pool.name')">
            <b-input v-model="form.name" :disabled="!!editing" required></b-input>
          </b-field>
          <b-field :label="$t('pool.outbound')">
            <b-input v-model="form.outbound" :placeholder="$t('pool.outboundHint')"></b-input>
          </b-field>
          <b-field :label="$t('pool.members')">
            <div v-for="n in candidates" :key="n.key" class="pool-member-line">
              <b-checkbox v-model="n.selected" @input="onToggleMember(n)">
                {{ n.label }}
              </b-checkbox>
              <template v-if="n.selected">
                <b-input v-model="n.agentURL" size="is-small" class="pool-member-agent" :placeholder="$t('pool.agentURL')"></b-input>
                <b-input v-model="n.agentToken" size="is-small" type="password" class="pool-member-token" :placeholder="$t('pool.agentToken')"></b-input>
              </template>
            </div>
          </b-field>
          <div class="columns">
            <div class="column">
              <b-field :label="$t('pool.trafficGatePct')">
                <b-input v-model.number="form.trafficGatePct" type="number" step="0.1" min="0" max="100"></b-input>
              </b-field>
            </div>
            <div class="column">
              <b-field :label="$t('pool.pollInterval')">
                <b-input v-model="form.pollInterval"></b-input>
              </b-field>
            </div>
          </div>
          <b-field>
            <b-checkbox v-model="form.failOpen">{{ $t("pool.failOpen") }}</b-checkbox>
          </b-field>
        </section>
        <footer class="modal-card-foot">
          <b-button type="is-primary" :loading="saving" @click="save">{{ $t("operations.save") }}</b-button>
          <b-button @click="editActive = false">{{ $t("operations.cancel") }}</b-button>
        </footer>
      </div>
    </b-modal>
  </div>
</template>

<script>
import axios from "@/plugins/axios";

function whichKey(w) {
  return `${w._type}:${w.id}:${w.sub || 0}`;
}

export default {
  name: "PoolPanel",
  data() {
    return {
      pools: [],
      candidates: [],
      editActive: false,
      editing: null, // 编辑中的池对象（null = 新建）
      form: {
        name: "",
        outbound: "",
        trafficGatePct: 90,
        pollInterval: "30s",
        failOpen: true,
      },
      saving: false,
      busy: {}, // busyKey -> true
      timer: null,
    };
  },
  mounted() {
    this.load();
    this.timer = setInterval(() => this.load(), 10000);
  },
  beforeDestroy() {
    if (this.timer) clearInterval(this.timer);
  },
  methods: {
    async load() {
      try {
        const res = await axios({ url: apiRoot + "/pools" });
        if (res.data && res.data.code === "SUCCESS") {
          this.pools = res.data.data.pools || [];
        }
      } catch (e) {
        // 错误 toast 由 axios 拦截器处理
      }
    },
    async loadCandidates() {
      try {
        const res = await axios({ url: apiRoot + "/touch" });
        if (!res.data || res.data.code !== "SUCCESS") return;
        const touch = res.data.data.touch;
        this.candidates = [];
        (touch.servers || []).forEach((s) => {
          this.candidates.push({
            key: `server:${s.id}`,
            label: `${s.name} (${s.address}, ${s.net})`,
            which: { _type: s._type, id: s.id, sub: 0 },
            selected: false,
            agentURL: "",
            agentToken: "",
          });
        });
        (touch.subscriptions || []).forEach((sub, si) => {
          (sub.servers || []).forEach((s) => {
            this.candidates.push({
              key: `subscriptionserver:${si}:${s.id}`,
              label: `${s.name} [${sub.remarks || sub.host}] (${s.address})`,
              which: { _type: s._type, id: s.id, sub: si },
              selected: false,
              agentURL: "",
              agentToken: "",
            });
          });
        });
      } catch (e) {
        // 忽略：候选列表为空时无法选择成员
      }
    },
    memberKey(w) {
      return whichKey(w);
    },
    memberName(w) {
      for (const n of this.candidates) {
        if (n.which._type === w._type && n.which.id === w.id && (n.which.sub || 0) === (w.sub || 0)) {
          return n.label;
        }
      }
      const p = this.pools.find((pp) => pp.members.some((m) => whichKey(m.which) === whichKey(w)));
      if (p) {
        const found = p.members.find((m) => whichKey(m.which) === whichKey(w));
        if (found && found.status && found.status.node && found.status.node.id) {
          return found.status.node.id;
        }
      }
      return `${w._type}#${w.id}`;
    },
    pctText(v) {
      return `${(v || 0).toFixed(1)}%`;
    },
    latencyText(m) {
      if (
        m.status &&
        m.status.latency &&
        typeof m.status.latency.ms === "number" &&
        m.status.latency.ms > 0
      ) {
        return `${Math.round(m.status.latency.ms)} ms`;
      }
      return "-";
    },
    bytesText(b) {
      const n = Number(b || 0);
      if (n >= 1 << 30) return (n / (1 << 30)).toFixed(2) + " GB";
      if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + " MB";
      if (n >= 1 << 10) return (n / (1 << 10)).toFixed(1) + " KB";
      return n + " B";
    },
    progressClass(usedPct, stopPct) {
      if (usedPct >= (stopPct || 95)) return "is-danger";
      if (usedPct >= 80) return "is-warning";
      return "is-success";
    },
    openEdit(pool) {
      this.editing = pool ? { name: pool.name } : null;
      this.form = {
        name: pool ? pool.name : "",
        outbound: pool ? pool.outbound || "" : "",
        trafficGatePct: pool ? pool.settings.trafficGatePct : 90,
        pollInterval: pool ? pool.settings.pollInterval : "30s",
        failOpen: pool ? pool.settings.failOpen : true,
      };
      this.loadCandidates().then(() => {
        const selected = {};
        (pool ? pool.members : []).forEach((m) => {
          selected[whichKey(m.which)] = { agentURL: m.agentURL, agentToken: m.agentToken };
        });
        this.candidates.forEach((n) => {
          const s = selected[whichKey(n.which)];
          if (s !== undefined) {
            n.selected = true;
            n.agentURL = s.agentURL;
            n.agentToken = s.agentToken;
          }
        });
      });
      this.editActive = true;
    },
    onToggleMember(n) {
      if (!n.selected) {
        n.agentURL = "";
        n.agentToken = "";
      }
    },
    buildPayload() {
      const members = this.candidates
        .filter((n) => n.selected)
        .map((n) => ({
          which: n.which,
          agentURL: (n.agentURL || "").trim(),
          agentToken: (n.agentToken || "").trim(),
          enabled: true,
        }));
      return {
        name: this.form.name.trim(),
        outbound: this.form.outbound.trim(),
        members,
        settings: {
          trafficGatePct: Number(this.form.trafficGatePct),
          pollInterval: this.form.pollInterval.trim() || "30s",
          failOpen: !!this.form.failOpen,
        },
      };
    },
    async save() {
      const payload = this.buildPayload();
      if (!payload.name) {
        this.$buefy.toast.open({ message: this.$t("pool.nameRequired"), type: "is-danger" });
        return;
      }
      if (!payload.members.length) {
        this.$buefy.toast.open({ message: this.$t("pool.membersRequired"), type: "is-danger" });
        return;
      }
      this.saving = true;
      try {
        const method = this.editing ? "put" : "post";
        const url = this.editing
          ? apiRoot + "/pools/" + encodeURIComponent(this.editing.name)
          : apiRoot + "/pools";
        const res = await axios({ url, method, data: payload });
        if (res.data && res.data.code === "SUCCESS") {
          this.editActive = false;
          this.load();
        } else if (res.data && res.data.message) {
          this.$buefy.toast.open({ message: res.data.message, type: "is-danger" });
        }
      } catch (e) {
        // 拦截器已提示
      } finally {
        this.saving = false;
      }
    },
    remove(pool) {
      this.$buefy.dialog.confirm({
        message: this.$t("pool.deleteConfirm", { name: pool.name }),
        confirmText: this.$t("operations.delete"),
        type: "is-danger",
        onConfirm: async () => {
          try {
            const res = await axios({
              url: apiRoot + "/pools/" + encodeURIComponent(pool.name),
              method: "delete",
            });
            if (res.data && res.data.code === "SUCCESS") this.load();
          } catch (e) {
            // 拦截器已提示
          }
        },
      });
    },
    busyKey(poolName, w, action) {
      return `${poolName}|${whichKey(w)}|${action}`;
    },
    async control(pool, m, action) {
      const key = this.busyKey(pool.name, m.which, action);
      this.$set(this.busy, key, true);
      try {
        const res = await axios({
          url: apiRoot + "/pools/" + encodeURIComponent(pool.name) + "/control",
          method: "post",
          data: { member: m.which, action },
        });
        if (res.data && res.data.code === "SUCCESS") {
          this.$buefy.toast.open({ message: this.$t("pool.controlOk", { action }), type: "is-success" });
        } else if (res.data && res.data.message) {
          this.$buefy.toast.open({ message: res.data.message, type: "is-danger" });
        }
      } catch (e) {
        // 拦截器已提示
      } finally {
        this.$delete(this.busy, key);
      }
    },
  },
};
</script>

<style scoped>
.pool-panel {
  width: 980px;
  max-width: 96vw;
}
.pool-empty {
  text-align: center;
  padding: 48px 16px;
  color: #7a7a7a;
}
.pool-empty__icon {
  font-size: 40px;
  margin-bottom: 8px;
}
.pool-empty__title {
  font-size: 16px;
  font-weight: 500;
  margin-bottom: 4px;
}
.pool-empty__desc {
  font-size: 13px;
  margin-bottom: 16px;
}
.pool-card__header {
  margin-bottom: 8px;
}
.pool-member-line {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}
.pool-member-line .b-checkbox {
  min-width: 260px;
  margin-right: 8px;
}
.pool-member-agent {
  flex: 1 1 220px;
}
.pool-member-token {
  flex: 1 1 220px;
}
.pool-edit-body {
  max-height: 70vh;
  overflow-y: auto;
}
</style>
