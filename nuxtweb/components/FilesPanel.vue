<script setup lang="ts">
import {
  Folder,
  FileText,
  ChevronRight,
  ArrowUp,
  FilePlus2,
  FolderPlus,
  RefreshCw,
  Pencil,
  Trash2,
  Lock,
  UserCog,
  Upload,
  Download,
  Save,
  X,
  Home,
} from 'lucide-vue-next'
import { Message, Dialog, Button, Input, Empty } from 'fuxsto-design'

interface FileEntry {
  name: string
  path: string
  isDir: boolean
  size: number
}

const path = ref('')
const root = ref('')
const entries = ref<FileEntry[]>([])
const loading = ref(false)

const editor = ref({
  open: false,
  path: '',
  content: '',
  saved: '',
})

const newName = ref('')
const newKind = ref<'' | 'file' | 'dir'>('')

async function loadDir(p?: string) {
  loading.value = true
  try {
    const q = p !== undefined ? `?path=${encodeURIComponent(p)}` : path.value ? `?path=${encodeURIComponent(path.value)}` : ''
    const res = await useApi<{ root: string; path: string; entries: FileEntry[] }>(`/api/files${q}`)
    root.value = res.root
    path.value = res.path
    entries.value = res.entries || []
  } catch (e: any) {
    Message.error(e?.message || '目录加载失败')
  } finally {
    loading.value = false
  }
}

async function loadWorkspace() {
  try {
    const res = await useApi<{ root: string }>('/api/workspace')
    await loadDir(res.root)
  } catch {
    loadDir()
  }
}

function gotoParent() {
  const norm = path.value.replace(/\\/g, '/')
  const idx = norm.lastIndexOf('/')
  if (idx <= 0) {
    loadDir('/')
    return
  }
  loadDir(norm.slice(0, idx))
}

const crumbs = computed(() => {
  const isWin = /^[a-zA-Z]:/.test(path.value)
  const norm = path.value.replace(/\\/g, '/')
  const segs = norm.split('/').filter(Boolean)
  const out: { name: string; path: string }[] = []
  let acc = ''
  segs.forEach((s, i) => {
    if (isWin && i === 0) {
      acc = s + '/'
      out.push({ name: s, path: acc })
    } else {
      acc = acc.replace(/\/$/, '') + '/' + s
      out.push({ name: s, path: acc })
    }
  })
  return out
})

function fmtSize(n: number): string {
  if (!n) return ''
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

async function openFile(p: string) {
  try {
    const res = await useApi<{ path: string; content: string }>(`/api/file?path=${encodeURIComponent(p)}`)
    editor.value = { open: true, path: res.path, content: res.content, saved: res.content }
  } catch (e: any) {
    Message.error(e?.message || '文件读取失败')
  }
}

const lineNums = computed(() => editor.value.content.split('\n').length)
const taRef = ref<HTMLTextAreaElement | null>(null)
const numsRef = ref<HTMLElement | null>(null)

function syncScroll() {
  if (numsRef.value && taRef.value) numsRef.value.scrollTop = taRef.value.scrollTop
}

function onTaKeydown(e: KeyboardEvent) {
  if (e.key === 'Tab') {
    e.preventDefault()
    const ta = e.target as HTMLTextAreaElement
    const { selectionStart: s, selectionEnd: en } = ta
    editor.value.content = editor.value.content.slice(0, s) + '  ' + editor.value.content.slice(en)
    nextTick(() => {
      ta.selectionStart = ta.selectionEnd = s + 2
    })
  }
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
    e.preventDefault()
    saveFile()
  }
}

async function saveFile() {
  try {
    await useApi('/api/file', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ path: editor.value.path, content: editor.value.content }),
    })
    editor.value.saved = editor.value.content
    Message.success('已保存')
  } catch (e: any) {
    Message.error(e?.message || '保存失败')
  }
}

function closeEditor() {
  if (editor.value.content !== editor.value.saved) {
    Dialog.warning({
      title: '未保存的修改',
      content: '文件有未保存的修改，确定关闭？',
      danger: true,
      confirmText: '关闭',
      onConfirm: () => {
        editor.value.open = false
      },
    })
  } else {
    editor.value.open = false
  }
}

function toggleNew(kind: 'file' | 'dir') {
  newKind.value = newKind.value === kind ? '' : kind
  newName.value = ''
}

async function confirmNew() {
  const name = newName.value.trim()
  if (!name) return
  const base = path.value.replace(/[\\/]+$/, '')
  const full = `${base}/${name}`
  try {
    if (newKind.value === 'file') {
      await useApi('/api/file', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ path: full, content: '' }),
      })
    } else {
      await useApi('/api/mkdir', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ path: full }),
      })
    }
    Message.success('创建成功')
    newKind.value = ''
    newName.value = ''
    await loadDir()
  } catch (e: any) {
    Message.error(e?.message || '创建失败')
  }
}

function removeEntry(p: string, isDir: boolean) {
  Dialog.confirm({
    title: '删除',
    content: `确定删除 ${p} ？`,
    danger: true,
    confirmText: '删除',
    onConfirm: async () => {
      try {
        await useApi('/api/delete', {
          method: 'POST',
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify({ path: p, recursive: false }),
        })
        Message.success('已删除')
        loadDir()
      } catch (e: any) {
        const msg = e?.message || ''
        if (isDir && msg.includes('非空')) {
          Dialog.confirm({
            title: '目录非空',
            content: '递归删除该目录下的全部内容？',
            danger: true,
            confirmText: '递归删除',
            onConfirm: async () => {
              try {
                await useApi('/api/delete', {
                  method: 'POST',
                  headers: { 'content-type': 'application/json' },
                  body: JSON.stringify({ path: p, recursive: true }),
                })
                Message.success('已递归删除')
                loadDir()
              } catch (e2: any) {
                Message.error(e2?.message || '删除失败')
              }
            },
          })
        } else {
          Message.error(msg || '删除失败')
        }
      }
    },
  })
}

function chmod(p: string) {
  Dialog.confirm({
    title: '修改权限 (chmod)',
    content: '输入八进制权限，如 644 / 755',
    showCancel: false,
    showConfirm: false,
    closable: true,
  })
  const mode = window.prompt('输入八进制权限（如 644 / 755）')
  if (!mode) return
  useApi('/api/chmod', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ path: p, mode: mode.trim() }),
  })
    .then(() => {
      Message.success('权限已修改')
    })
    .catch((e: any) => Message.error(e?.message || '修改失败'))
}

function chown(p: string) {
  const owner = window.prompt('输入 uid:gid（-1 表示保持不变，如 1000:1000）')
  if (!owner) return
  useApi('/api/chown', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ path: p, owner: owner.trim() }),
  })
    .then(() => {
      Message.success('属主已修改')
    })
    .catch((e: any) => Message.error(e?.message || '修改失败'))
}

const uploadRef = ref<HTMLInputElement | null>(null)

async function onUpload(e: Event) {
  const files = [...((e.target as HTMLInputElement).files || [])]
  ;(e.target as HTMLInputElement).value = ''
  if (!files.length) return
  for (const file of files) {
    try {
      const fd = new FormData()
      fd.append('dir', path.value || '/')
      fd.append('file', file)
      const res = await fetch('/api/upload', {
        method: 'POST',
        body: fd,
        headers: { Accept: 'application/json' },
      })
      const data = await res.json().catch(() => ({}))
      if (data.path) {
        Message.success(`已上传 ${file.name} → ${data.path}`)
      } else {
        Message.error(`${file.name}：${data.error || '上传失败'}`)
      }
    } catch (e: any) {
      Message.error(`${file.name}：${e?.message || '上传失败'}`)
    }
  }
  loadDir()
}

function downloadPath(p: string) {
  const a = document.createElement('a')
  a.href = `/api/download?path=${encodeURIComponent(p)}`
  document.body.appendChild(a)
  a.click()
  a.remove()
}

async function setWorkspace() {
  const p = window.prompt('设置工作目录（绝对路径）', root.value)
  if (!p) return
  try {
    await useApi('/api/workspace', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ path: p.trim() }),
    })
    Message.success('工作目录已更新')
    loadDir(p.trim())
  } catch (e: any) {
    Message.error(e?.message || '设置失败')
  }
}

onMounted(loadWorkspace)
</script>

<template>
  <div class="flex flex-col text-sm">
    <div class="space-y-2 p-3">
      <div class="flex gap-1.5">
        <Input v-model="path" size="sm" placeholder="绝对路径，回车浏览" class="flex-1" @keydown.enter="loadDir(path)" />
        <Button size="sm" variant="outline" :loading="loading" @click="loadDir(path)">浏览</Button>
        <Button size="sm" variant="ghost" :icon="Home" title="工作目录" @click="loadWorkspace" />
        <Button size="sm" variant="ghost" :icon="RefreshCw" title="刷新" @click="loadDir()" />
      </div>
      <div class="flex flex-wrap gap-1.5">
        <Button size="sm" variant="ghost" :icon="FilePlus2" @click="toggleNew('file')">文件</Button>
        <Button size="sm" variant="ghost" :icon="FolderPlus" @click="toggleNew('dir')">文件夹</Button>
        <Button size="sm" variant="ghost" :icon="Upload" title="上传（支持多选，上传到当前目录）" @click="uploadRef?.click()">上传</Button>
        <input ref="uploadRef" type="file" multiple class="hidden" @change="onUpload" />
        <span class="flex-1" />
        <Button size="sm" variant="ghost" :icon="RefreshCw" title="刷新" @click="loadDir()" />
        <Button size="sm" variant="ghost" title="设置工作目录" @click="setWorkspace">工作目录</Button>
      </div>
      <div v-if="newKind" class="flex gap-1.5">
        <Input
          v-model="newName"
          size="sm"
          autofocus
          :placeholder="newKind === 'file' ? '新文件名' : '新文件夹名'"
          class="flex-1"
          @keydown.enter="confirmNew"
        />
        <Button size="sm" variant="primary" @click="confirmNew">创建</Button>
      </div>
      <div class="flex flex-wrap items-center gap-0.5 text-xs text-zinc-500">
        <button class="hover:text-zinc-900 dark:hover:text-zinc-100" @click="loadDir(root)">{{ root }}</button>
        <template v-for="(c, i) in crumbs" :key="c.path">
          <ChevronRight :size="11" class="text-zinc-300" />
          <button class="max-w-32 truncate hover:text-zinc-900 dark:hover:text-zinc-100" :title="c.path" @click="loadDir(c.path)">
            {{ c.name }}
          </button>
          <span v-if="i === crumbs.length - 1" class="flex-1" />
        </template>
        <span v-if="!crumbs.length" class="flex-1" />
        <button
          v-if="crumbs.length"
          class="flex items-center gap-0.5 hover:text-zinc-900 dark:hover:text-zinc-100"
          title="上级目录"
          @click="gotoParent"
        >
          <ArrowUp :size="12" /> 上级
        </button>
      </div>
    </div>

    <div class="min-h-0 flex-1 overflow-y-auto border-t border-zinc-200 dark:border-zinc-800">
      <Empty v-if="!entries.length && !loading" title="空目录" size="sm" class="mt-6" />
      <div
        v-for="en in entries"
        :key="en.path"
        class="group flex items-center gap-2 px-3 py-1.5 text-[13px] hover:bg-zinc-50 dark:hover:bg-zinc-800/60"
      >
        <component
          :is="en.isDir ? Folder : FileText"
          :size="14"
          class="shrink-0"
          :class="en.isDir ? 'text-amber-500' : 'text-zinc-400'"
        />
        <button
          class="min-w-0 flex-1 truncate text-left hover:underline"
          :title="en.path"
          @click="en.isDir ? loadDir(en.path) : openFile(en.path)"
        >
          {{ en.name }}
        </button>
        <span class="shrink-0 text-[10px] tabular-nums text-zinc-400">{{ fmtSize(en.size) }}</span>
        <span class="hidden shrink-0 items-center group-hover:flex">
          <button class="p-1 text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200" :title="en.isDir ? '打包下载 zip' : '下载'" @click="downloadPath(en.path)">
            <Download :size="12" />
          </button>
          <button v-if="!en.isDir" class="p-1 text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200" title="编辑" @click="openFile(en.path)">
            <Pencil :size="12" />
          </button>
          <button class="p-1 text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200" title="chmod" @click="chmod(en.path)">
            <Lock :size="12" />
          </button>
          <button class="p-1 text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200" title="chown" @click="chown(en.path)">
            <UserCog :size="12" />
          </button>
          <button class="p-1 text-zinc-400 hover:text-red-500" title="删除" @click="removeEntry(en.path, en.isDir)">
            <Trash2 :size="12" />
          </button>
        </span>
      </div>
    </div>

    <div v-if="editor.open" class="shrink-0 border-t border-zinc-200 dark:border-zinc-800">
      <div class="flex items-center gap-2 border-b border-zinc-200 px-3 py-1.5 dark:border-zinc-800">
        <span class="min-w-0 flex-1 truncate font-mono text-[11px] text-zinc-500" :title="editor.path">
          {{ editor.path }}<span v-if="editor.content !== editor.saved" class="text-amber-500"> •</span>
        </span>
        <Button size="sm" variant="primary" :icon="Save" @click="saveFile">保存</Button>
        <Button size="sm" variant="ghost" :icon="X" @click="closeEditor" />
      </div>
      <div class="flex" style="max-height: 45vh">
        <div
          ref="numsRef"
          class="shrink-0 select-none overflow-hidden border-r border-zinc-200 bg-zinc-50 px-1.5 py-2 text-right font-mono text-[11px] leading-[1.6] text-zinc-400 dark:border-zinc-800 dark:bg-zinc-900"
        >
          <div v-for="i in lineNums" :key="i">{{ i }}</div>
        </div>
        <textarea
          ref="taRef"
          v-model="editor.content"
          spellcheck="false"
          class="min-h-40 flex-1 resize-none bg-transparent p-2 font-mono text-[11px] leading-[1.6] outline-none"
          style="tab-size: 2"
          @scroll="syncScroll"
          @keydown="onTaKeydown"
        />
      </div>
    </div>
  </div>
</template>
