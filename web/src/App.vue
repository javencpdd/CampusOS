<template>
  <el-config-provider :locale="zhCn">
    <CampusApp />
  </el-config-provider>
</template>

<script setup lang="ts">
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import CampusApp from '@/campus-ui/CampusApp.vue'
</script>
