<template>
  <section class="mfa-policy">
    <header>
      <h2>管理员 MFA 安全配置</h2>
      <el-button :loading="loading" @click="refresh">刷新生效状态</el-button>
    </header>
    <template v-if="admin.profile">
      <p>
        <strong
          >当前部署：{{
            required ? "已开启二次验证" : "已关闭二次验证"
          }}</strong
        >
      </p>
      <p>
        当前管理员认证器：{{
          admin.profile.mfa_enrolled ? "已配置" : "未配置"
        }}。
      </p>
      <el-alert
        v-if="!required"
        type="warning"
        :closable="false"
        title="关闭后只使用密码认证与原有权限校验，账号防护降低；生产或局域网环境建议开启。"
      />
      <el-alert
        v-else
        type="info"
        :closable="false"
        title="敏感操作需要 6 位认证器验证码。验证有效期 15 分钟，过期后重新确认身份。"
      />
      <h3>如何开启或关闭</h3>
      <p>
        这是部署级配置，由部署者修改；本页展示当前服务端实际状态，不使用旧用户域登录策略控制独立管理员。
      </p>
      <p>
        在 Docker 开发配置 <code>deploy/docker/.env.dev.local</code> 中设置：
      </p>
      <pre>AUTH_ADMIN_MFA_REQUIRED={{ required ? "true" : "false" }}</pre>
      <p>
        <code>true</code> 开启（默认），<code>false</code>
        关闭。修改后在仓库根目录执行：
      </p>
      <pre>.\scripts\docker-dev.ps1 up</pre>
      <p>
        Linux 使用
        <code>./scripts/docker-dev.sh up</code>。应用配置后刷新本页；不需要因此
        rebuild，也不要删除 MFA 加密密钥。
      </p>
      <p>
        关闭不会删除认证器绑定，不会修改登录密码或将密码会话伪装成
        MFA；角色、资源权限、CSRF 和操作审计仍然生效。
      </p>
      <template v-if="required">
        <h3>认证器使用引导</h3>
        <ol>
          <li>准备支持 TOTP 的认证器；点击下方按钮开始配置。</li>
          <li>
            在认证器中使用添加链接，或手动添加页面显示的密钥，选择基于时间的动态验证码。
          </li>
          <li>
            输入 6 位验证码完成绑定；随后等待下一组验证码，再确认管理员身份。
          </li>
          <li>
            验证通过后重试原操作；验证码已使用或过期时，等待下一组，并检查设备自动同步时间。
          </li>
        </ol>
        <p>
          不要分享添加链接、绑定密钥或动态验证码；认证器丢失时由部署者使用独立管理员离线恢复流程。
        </p>
        <el-button type="primary" @click="mfaOpen = true">{{
          admin.profile.mfa_enrolled ? "确认管理员身份" : "开始配置认证器"
        }}</el-button>
      </template>
    </template>
    <p v-else>请先登录独立管理端，再查看当前 MFA 配置。</p>
    <AdminSelfMFA v-model="mfaOpen" />
  </section>
</template>
<script setup lang="ts">
import { computed, ref } from "vue";
import { ElMessage } from "element-plus";
import { useAdminStore } from "@/modules/identity/store";
import AdminSelfMFA from "@/components/AdminSelfMFA.vue";
const admin = useAdminStore();
const required = computed(() => admin.profile?.mfa_required !== false);
const loading = ref(false);
const mfaOpen = ref(false);
async function refresh() {
  if (loading.value) return;
  loading.value = true;
  try {
    if (!(await admin.restore()))
      throw new Error("管理员会话已失效，请重新登录");
  } catch (error: any) {
    ElMessage.error(error?.msg || error?.message || "MFA 配置状态加载失败");
  } finally {
    loading.value = false;
  }
}
</script>
<style scoped>
.mfa-policy {
  max-width: 920px;
  margin: 0 auto;
  padding-bottom: 28px;
  overflow-wrap: anywhere;
}
header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
pre {
  padding: 12px;
  overflow-x: auto;
  background: #f5f7fa;
}
li {
  margin-bottom: 8px;
}
</style>
