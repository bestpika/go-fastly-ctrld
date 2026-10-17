<!-- Sakai Vue 5.0.0，Copyright 2018-2022 PrimeTek，MIT；固定來源見 sakai-source.json。 -->
<script setup lang="ts">
import { computed } from 'vue'
import type { MenuItem } from 'primevue/menuitem'
import { useLayout } from './composables/layout'

const { layoutState, isDesktop, closeMenu } = useLayout()
const props = withDefaults(
  defineProps<{
    item: MenuItem
    root?: boolean
    parentPath?: string | null
  }>(),
  { root: true, parentPath: null },
)
const fullPath = computed<string | null>(() =>
  props.item.path
    ? props.parentPath
      ? props.parentPath + props.item.path
      : props.item.path
    : null,
)
const isActive = computed(() =>
  props.item.path
    ? !!fullPath.value && !!layoutState.activePath?.startsWith(fullPath.value)
    : layoutState.activePath === props.item.to,
)
const isDisabled = computed(() =>
  typeof props.item.disabled === 'function'
    ? props.item.disabled()
    : !!props.item.disabled,
)
function itemClick(event: MouseEvent | KeyboardEvent, item: MenuItem) {
  if (isDisabled.value) {
    event.preventDefault()
    return
  }
  item.command?.({ originalEvent: event, item })
  if (item.items) {
    if (isActive.value) layoutState.activePath = props.parentPath
    else {
      layoutState.activePath = fullPath.value
      layoutState.menuHoverActive = true
    }
  } else closeMenu()
}
function onMouseEnter() {
  if (
    isDesktop() &&
    props.root &&
    props.item.items &&
    layoutState.menuHoverActive
  )
    layoutState.activePath = fullPath.value
}
</script>

<template>
  <li :class="{ 'layout-root-menuitem': root, 'active-menuitem': isActive }">
    <div
      v-if="root && item.visible !== false"
      class="layout-menuitem-root-text"
    >
      {{ item.label }}
    </div>
    <a
      v-if="(!item.to || item.items) && item.visible !== false"
      :href="item.url"
      :class="item.class"
      :target="item.target"
      tabindex="0"
      :role="item.items ? 'button' : undefined"
      :aria-expanded="item.items ? root || isActive : undefined"
      :aria-disabled="isDisabled || undefined"
      @click="itemClick($event, item)"
      @keydown.enter.prevent="item.items && itemClick($event, item)"
      @keydown.space.prevent="item.items && itemClick($event, item)"
      @mouseenter="onMouseEnter"
    >
      <i :class="item.icon" class="layout-menuitem-icon" aria-hidden="true" />
      <span class="layout-menuitem-text">{{ item.label }}</span>
      <i
        v-if="item.items"
        class="pi pi-fw pi-angle-down layout-submenu-toggler"
        aria-hidden="true"
      />
    </a>
    <RouterLink
      v-if="item.to && !item.items && item.visible !== false"
      exact-active-class="active-route"
      :class="item.class"
      :aria-disabled="isDisabled || undefined"
      tabindex="0"
      :to="item.to"
      @click.capture="isDisabled && $event.preventDefault()"
      @click="itemClick($event, item)"
      @mouseenter="onMouseEnter"
    >
      <i :class="item.icon" class="layout-menuitem-icon" aria-hidden="true" />
      <span class="layout-menuitem-text">{{ item.label }}</span>
    </RouterLink>
    <Transition
      v-if="item.items && item.visible !== false"
      name="layout-submenu"
    >
      <ul v-show="root || isActive" class="layout-submenu">
        <AppMenuItem
          v-for="child in item.items"
          :key="child.label + '_' + (child.to || child.path)"
          :item="child"
          :root="false"
          :parent-path="fullPath"
        />
      </ul>
    </Transition>
  </li>
</template>
