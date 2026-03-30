<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.identityFingerprint.title')"
    width="wide"
    @close="handleClose"
  >
    <div class="space-y-4">
      <!-- Loading -->
      <div v-if="loading" class="flex items-center justify-center py-8">
        <LoadingSpinner />
      </div>

      <!-- Error -->
      <div v-else-if="error" class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800/40 dark:bg-red-900/20 dark:text-red-300">
        {{ error }}
      </div>

      <!-- No cache -->
      <div v-else-if="data && !data.cached" class="rounded-lg border border-gray-200 bg-gray-50 p-4 text-sm text-gray-600 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-400">
        {{ t('admin.accounts.identityFingerprint.noCache') }}
      </div>

      <!-- Fingerprint data -->
      <div v-else-if="data?.fingerprint" class="space-y-3">
        <div
          v-for="field in fingerprintFields"
          :key="field.key"
          class="flex flex-col gap-1 rounded-lg border border-gray-100 bg-gray-50/50 px-3 py-2 dark:border-gray-700/50 dark:bg-gray-800/50"
        >
          <span class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ field.label }}</span>
          <span class="break-all font-mono text-sm text-gray-900 dark:text-gray-100">{{ field.value || '-' }}</span>
        </div>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { adminAPI } from '@/api/admin'
import type { IdentityFingerprintResponse } from '@/api/admin/accounts'
import type { Account } from '@/types'

const props = defineProps<{ show: boolean; account: Account | null }>()
const emit = defineEmits(['close'])
const { t } = useI18n()

const loading = ref(false)
const error = ref<string | null>(null)
const data = ref<IdentityFingerprintResponse | null>(null)

const fingerprintFields = computed(() => {
  const fp = data.value?.fingerprint
  if (!fp) return []
  return [
    { key: 'client_id', label: 'Client ID', value: fp.client_id },
    { key: 'user_agent', label: 'User-Agent', value: fp.user_agent },
    { key: 'stainless_package_version', label: 'X-Stainless-Package-Version', value: fp.stainless_package_version },
    { key: 'stainless_os', label: 'X-Stainless-OS', value: fp.stainless_os },
    { key: 'stainless_arch', label: 'X-Stainless-Arch', value: fp.stainless_arch },
    { key: 'stainless_runtime', label: 'X-Stainless-Runtime', value: fp.stainless_runtime },
    { key: 'stainless_runtime_version', label: 'X-Stainless-Runtime-Version', value: fp.stainless_runtime_version },
    { key: 'stainless_lang', label: 'X-Stainless-Lang', value: fp.stainless_lang },
    { key: 'updated_at', label: t('admin.accounts.identityFingerprint.updatedAt'), value: fp.updated_at },
  ]
})

const fetchFingerprint = async () => {
  if (!props.account) return
  loading.value = true
  error.value = null
  data.value = null
  try {
    data.value = await adminAPI.accounts.getIdentityFingerprint(props.account.id)
  } catch (e: any) {
    error.value = e?.response?.data?.message || e?.message || 'Failed to load'
  } finally {
    loading.value = false
  }
}

watch(() => props.show, (visible) => {
  if (visible && props.account) {
    fetchFingerprint()
  }
})

const handleClose = () => {
  emit('close')
}
</script>
