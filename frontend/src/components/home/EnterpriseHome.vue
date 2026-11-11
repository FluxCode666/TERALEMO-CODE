<template>
  <div ref="rootRef" class="tl-home" data-testid="enterprise-home">
    <a href="#tl-main" class="tl-skip" @click.prevent="scrollToSection('tl-main')">{{ t('home.skipToContent') }}</a>
    <div class="tl-grain" aria-hidden="true" />

    <!-- ============ Header ============ -->
    <header class="tl-header" :class="{ 'is-scrolled': scrolled }">
      <div class="tl-container tl-header-inner">
        <a href="#" class="tl-brand" :aria-label="siteName" @click.prevent="scrollToTop">
          <img v-if="siteLogo" :src="siteLogo" alt="" class="tl-brand-logo" />
          <svg v-else class="tl-brand-glyph" viewBox="0 0 32 32" aria-hidden="true">
            <path d="M16 3.5c7.6 0 12.5 4.6 12.5 11.6 0 7.7-5.6 13.4-12.9 13.4C8.4 28.5 3.5 23.3 3.5 16 3.5 8.7 8.8 3.5 16 3.5Z" />
            <path d="M16.2 8.6c4.5 0 7.3 2.8 7.3 6.8 0 4.4-3.2 7.8-7.6 7.8-4.2 0-7-3.1-7-7.2 0-4.3 3-7.4 7.3-7.4Z" />
            <circle cx="15.8" cy="15.6" r="2.4" class="tl-brand-peak" />
          </svg>
          <span class="tl-wordmark">{{ siteName }}</span>
        </a>

        <nav class="tl-nav" :aria-label="t('home.nav.label')">
          <a v-for="item in navItems" :key="item.id" :href="`#${item.id}`" @click.prevent="scrollToSection(item.id)">
            {{ item.label }}
          </a>
        </nav>

        <div class="tl-actions">
          <LocaleSwitcher />
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="tl-icon-btn"
            :title="t('home.viewDocs')"
            :aria-label="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>
          <router-link v-if="showModelPlazaEntry" to="/model-plaza" class="tl-icon-btn tl-icon-btn--text" :title="t('nav.modelPlaza')">
            <Icon name="grid" size="md" />
            <span class="tl-hide-mobile">{{ t('nav.modelPlaza') }}</span>
          </router-link>
          <button
            type="button"
            class="tl-icon-btn"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="emit('toggle-theme')"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
          <router-link :to="primaryTo" class="tl-btn tl-btn--ink tl-btn--sm">
            {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          </router-link>
        </div>
      </div>
    </header>

    <main id="tl-main" tabindex="-1">
      <!-- ============ Hero ============ -->
      <section class="tl-hero">
        <div class="tl-hero-grid" aria-hidden="true" />
        <div class="tl-container tl-hero-inner">
          <div class="tl-hero-copy">
            <p class="tl-eyebrow tl-rise" style="--d: 0">
              <span class="tl-eyebrow-mark" aria-hidden="true" />
              {{ t('home.hero.eyebrow') }}
              <span class="tl-eyebrow-coord" aria-hidden="true">EL 00—08</span>
            </p>
            <h1 class="tl-title">
              <span class="tl-title-line tl-rise" style="--d: 1">{{ t('home.hero.titleLead') }}</span>
              <span class="tl-title-line tl-rise" style="--d: 2">
                <span class="tl-title-accent">{{ t('home.hero.titleAccent') }}</span>
              </span>
            </h1>
            <p class="tl-lede tl-rise" style="--d: 3">{{ t('home.hero.description', { name: siteName }) }}</p>
            <div class="tl-hero-ctas tl-rise" style="--d: 4">
              <router-link :to="primaryTo" class="tl-btn tl-btn--ink tl-btn--lg">
                {{ isAuthenticated ? t('home.hero.primaryAuthed') : t('home.hero.primaryGuest') }}
                <Icon name="arrowRight" size="sm" :stroke-width="2" class="tl-btn-arrow" />
              </router-link>
              <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="tl-btn tl-btn--ghost tl-btn--lg">
                {{ t('home.hero.secondaryDocs') }}
                <Icon name="externalLink" size="sm" />
              </a>
              <router-link v-else-if="showModelPlazaEntry" to="/model-plaza" class="tl-btn tl-btn--ghost tl-btn--lg">
                {{ t('home.hero.secondaryPlaza') }}
              </router-link>
              <a v-else href="#onboarding" class="tl-btn tl-btn--ghost tl-btn--lg" @click.prevent="scrollToSection('onboarding')">
                {{ t('home.hero.secondaryOnboarding') }}
              </a>
            </div>
            <p class="tl-hero-note tl-rise" style="--d: 5">
              <Icon name="users" size="sm" />
              {{ t('home.hero.note') }}
            </p>
          </div>
          <div class="tl-hero-map">
            <TerrainMap />
          </div>
        </div>
      </section>

      <!-- ============ Facts ============ -->
      <section class="tl-facts" :aria-label="t('home.facts.label')">
        <div class="tl-container">
          <dl class="tl-facts-grid">
            <div v-for="(fact, index) in facts" :key="fact.id" class="tl-fact tl-reveal" :style="{ '--d': index }">
              <dt class="tl-fact-label">{{ fact.label }}</dt>
              <dd class="tl-fact-value">
                <span class="tl-fact-num">{{ fact.value }}</span>
                <span class="tl-fact-unit">{{ fact.unit }}</span>
              </dd>
              <dd class="tl-fact-desc">{{ fact.desc }}</dd>
            </div>
          </dl>
        </div>
      </section>

      <!-- ============ 01 Access ============ -->
      <section id="access" class="tl-section">
        <div class="tl-container">
          <header class="tl-section-head tl-reveal">
            <span class="tl-section-index" aria-hidden="true">01</span>
            <div class="tl-section-titles">
              <p class="tl-kicker">{{ t('home.access.kicker') }}</p>
              <h2 class="tl-h2">{{ t('home.access.title') }}</h2>
            </div>
            <p class="tl-section-desc">{{ t('home.access.description') }}</p>
          </header>

          <div class="tl-split">
            <ol class="tl-points">
              <li v-for="(point, index) in accessPoints" :key="point.id" class="tl-point tl-reveal" :style="{ '--d': index }">
                <span class="tl-point-index">{{ String(index + 1).padStart(2, '0') }}</span>
                <div>
                  <h3 class="tl-h3">{{ point.title }}</h3>
                  <p class="tl-body">{{ point.desc }}</p>
                </div>
              </li>
            </ol>

            <div class="tl-panel tl-reveal" style="--d: 1">
              <div class="tl-panel-head">
                <span class="tl-panel-lights" aria-hidden="true"><i /><i /><i /></span>
                <span>{{ t('home.access.panel.title') }}</span>
              </div>
              <div class="tl-panel-base">
                <span class="tl-panel-label">{{ t('home.access.panel.baseUrl') }}</span>
                <code class="tl-panel-url" data-testid="home-api-base-url">{{ apiBaseUrl }}</code>
                <button type="button" class="tl-panel-copy" @click="copyBaseUrl">
                  <Icon :name="copied ? 'check' : 'copy'" size="xs" />
                  <span aria-live="polite">{{ copied ? t('home.access.panel.copied') : t('home.access.panel.copy') }}</span>
                </button>
              </div>
              <div class="tl-panel-scroll">
                <table class="tl-endpoints">
                  <thead>
                    <tr>
                      <th scope="col">{{ t('home.access.panel.protocol') }}</th>
                      <th scope="col">{{ t('home.access.panel.endpoint') }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="endpoint in endpoints" :key="endpoint.path">
                      <td class="tl-endpoint-protocol">{{ endpoint.protocol }}</td>
                      <td>
                        <span class="tl-endpoint-method" :class="{ 'is-get': endpoint.method === 'GET' }">{{ endpoint.method }}</span>
                        <span class="tl-endpoint-path">{{ endpoint.path }}</span>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <p class="tl-panel-note">
                <Icon name="key" size="xs" />
                {{ t('home.access.panel.keyNote') }}
              </p>
            </div>
          </div>
        </div>
      </section>

      <!-- ============ 02 Governance ============ -->
      <section id="governance" class="tl-section tl-section--tint">
        <div class="tl-container">
          <header class="tl-section-head tl-reveal">
            <span class="tl-section-index" aria-hidden="true">02</span>
            <div class="tl-section-titles">
              <p class="tl-kicker">{{ t('home.governance.kicker') }}</p>
              <h2 class="tl-h2">{{ t('home.governance.title') }}</h2>
            </div>
            <p class="tl-section-desc">{{ t('home.governance.description') }}</p>
          </header>

          <div class="tl-split tl-split--reverse">
            <div class="tl-sheet tl-reveal" aria-hidden="true">
              <div class="tl-sheet-head">
                <span>{{ t('home.governance.table.title') }}</span>
                <span class="tl-mono">USD</span>
              </div>
              <div class="tl-sheet-cols">
                <span>{{ t('home.governance.table.department') }}</span>
                <span>{{ t('home.governance.table.usage') }}</span>
              </div>
              <ul class="tl-sheet-rows">
                <li v-for="row in quotaRows" :key="row.id" class="tl-row" :class="{ 'is-child': row.child }">
                  <span class="tl-row-name">{{ row.name }}</span>
                  <span class="tl-row-bar">
                    <span class="tl-row-fill" :class="`is-${row.status}`" :style="{ '--w': `${row.percent}%` }" />
                  </span>
                  <span class="tl-row-num">{{ row.used }} / {{ row.limit }}</span>
                  <span class="tl-chip" :class="`is-${row.status}`">{{ row.statusLabel }}</span>
                </li>
              </ul>
              <p class="tl-sheet-foot">{{ t('home.governance.table.footnote') }}</p>
            </div>

            <div class="tl-feature-grid">
              <article v-for="(item, index) in governanceItems" :key="item.id" class="tl-feature tl-reveal" :style="{ '--d': index }">
                <span class="tl-feature-icon"><Icon :name="item.icon" size="md" /></span>
                <h3 class="tl-h3">{{ item.title }}</h3>
                <p class="tl-body">{{ item.desc }}</p>
              </article>
            </div>
          </div>
        </div>
      </section>

      <!-- ============ 03 Security ============ -->
      <section id="security" class="tl-section tl-band">
        <div class="tl-band-contours" aria-hidden="true" />
        <div class="tl-container">
          <header class="tl-section-head tl-reveal">
            <span class="tl-section-index" aria-hidden="true">03</span>
            <div class="tl-section-titles">
              <p class="tl-kicker">{{ t('home.security.kicker') }}</p>
              <h2 class="tl-h2">{{ t('home.security.title') }}</h2>
            </div>
            <p class="tl-section-desc">{{ t('home.security.description') }}</p>
          </header>

          <div class="tl-security">
            <ul class="tl-security-grid">
              <li v-for="(item, index) in securityItems" :key="item.id" class="tl-security-item tl-reveal" :style="{ '--d': index }">
                <Icon :name="item.icon" size="md" class="tl-security-icon" />
                <div>
                  <h3 class="tl-h3">{{ item.title }}</h3>
                  <p class="tl-body">{{ item.desc }}</p>
                </div>
              </li>
            </ul>

            <div class="tl-ledger tl-reveal" style="--d: 2" aria-hidden="true">
              <div class="tl-ledger-head">
                <span>{{ t('home.security.ledger.title') }}</span>
                <span class="tl-ledger-live"><i />{{ t('home.security.ledger.live') }}</span>
              </div>
              <ol class="tl-ledger-rows">
                <li v-for="row in ledgerRows" :key="row.time" class="tl-ledger-row" :class="{ 'is-blocked': row.blocked }">
                  <span class="tl-ledger-time">{{ row.time }}</span>
                  <span class="tl-ledger-op">{{ row.op }}</span>
                  <span class="tl-ledger-actor">{{ row.actor }}</span>
                  <span class="tl-ledger-status">{{ row.status }}</span>
                  <span class="tl-ledger-meta">{{ row.meta }}</span>
                </li>
              </ol>
            </div>
          </div>
        </div>
      </section>

      <!-- ============ 04 Onboarding ============ -->
      <section id="onboarding" class="tl-section">
        <div class="tl-container">
          <header class="tl-section-head tl-section-head--compact tl-reveal">
            <span class="tl-section-index" aria-hidden="true">04</span>
            <div class="tl-section-titles">
              <p class="tl-kicker">{{ t('home.onboarding.kicker') }}</p>
              <h2 class="tl-h2">{{ t('home.onboarding.title') }}</h2>
            </div>
          </header>

          <ol class="tl-steps">
            <li v-for="(step, index) in steps" :key="step.id" class="tl-step tl-reveal" :style="{ '--d': index }">
              <span class="tl-step-node" aria-hidden="true">
                <span class="tl-step-num">{{ String(index + 1).padStart(2, '0') }}</span>
              </span>
              <h3 class="tl-h3">{{ step.title }}</h3>
              <p class="tl-body">{{ step.desc }}</p>
            </li>
          </ol>
        </div>
      </section>

      <!-- ============ CTA ============ -->
      <section class="tl-cta">
        <div class="tl-container">
          <div class="tl-cta-card tl-reveal">
            <svg class="tl-cta-contours" viewBox="0 0 600 400" preserveAspectRatio="xMaxYMid slice" aria-hidden="true">
              <ellipse v-for="n in 7" :key="n" cx="520" cy="200" :rx="n * 46" :ry="n * 34" />
            </svg>
            <div class="tl-cta-copy">
              <h2 class="tl-h2 tl-cta-title">{{ t('home.cta.title') }}</h2>
              <p class="tl-cta-desc">{{ t('home.cta.description') }}</p>
            </div>
            <div class="tl-cta-actions">
              <router-link :to="primaryTo" class="tl-btn tl-btn--signal tl-btn--lg">
                {{ isAuthenticated ? t('home.hero.primaryAuthed') : t('home.hero.primaryGuest') }}
                <Icon name="arrowRight" size="sm" :stroke-width="2" class="tl-btn-arrow" />
              </router-link>
              <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="tl-btn tl-btn--outline-light tl-btn--lg">
                {{ t('home.docs') }}
              </a>
              <p v-if="contactInfo" class="tl-cta-contact" data-testid="home-contact">
                <span class="tl-cta-contact-label">{{ t('home.cta.contact') }}</span>
                <a v-if="contactHref" :href="contactHref" target="_blank" rel="noopener noreferrer">{{ contactInfo }}</a>
                <span v-else>{{ contactInfo }}</span>
              </p>
            </div>
          </div>
        </div>
      </section>
    </main>

    <!-- ============ Footer ============ -->
    <footer class="tl-footer">
      <div class="tl-container">
        <div class="tl-footer-top">
          <div class="tl-footer-brand">
            <span class="tl-wordmark">{{ siteName }}</span>
            <p>{{ t('home.footer.tagline') }}</p>
          </div>
          <nav class="tl-footer-cols" :aria-label="t('home.nav.label')">
            <div>
              <p class="tl-footer-title">{{ t('home.footer.product') }}</p>
              <ul>
                <li v-for="item in navItems" :key="item.id">
                  <a :href="`#${item.id}`" @click.prevent="scrollToSection(item.id)">{{ item.label }}</a>
                </li>
                <li v-if="showModelPlazaEntry"><router-link to="/model-plaza">{{ t('nav.modelPlaza') }}</router-link></li>
                <li v-if="docUrl"><a :href="docUrl" target="_blank" rel="noopener noreferrer">{{ t('home.docs') }}</a></li>
              </ul>
            </div>
            <div>
              <p class="tl-footer-title">{{ t('home.footer.account') }}</p>
              <ul>
                <li>
                  <router-link :to="primaryTo">{{ isAuthenticated ? t('home.dashboard') : t('home.login') }}</router-link>
                </li>
                <li><router-link to="/key-usage">{{ t('home.footer.keyUsage') }}</router-link></li>
              </ul>
            </div>
          </nav>
        </div>
        <div class="tl-footer-bottom">
          <span>&copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}</span>
          <span class="tl-mono" aria-hidden="true">EL 00—08 · N/S/E/W</span>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import '@fontsource-variable/bricolage-grotesque/wdth.css'
import '@fontsource/ibm-plex-mono/latin-400.css'
import '@fontsource/ibm-plex-mono/latin-500.css'

import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import TerrainMap from './TerrainMap.vue'

type IconName = InstanceType<typeof Icon>['$props']['name']

const props = defineProps<{
  siteName: string
  siteLogo: string
  docUrl: string
  contactInfo: string
  apiBaseUrl: string
  isAuthenticated: boolean
  dashboardPath: string
  showModelPlazaEntry: boolean
  isDark: boolean
}>()

const emit = defineEmits<{ (event: 'toggle-theme'): void }>()

const { t } = useI18n()
const { copied, copyToClipboard } = useClipboard()

const rootRef = ref<HTMLElement | null>(null)
const scrolled = ref(false)
const currentYear = new Date().getFullYear()

const primaryTo = computed(() => (props.isAuthenticated ? props.dashboardPath : '/login'))

const contactHref = computed(() => {
  const value = props.contactInfo.trim()
  if (/^https?:\/\/\S+$/i.test(value)) return value
  if (/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)) return `mailto:${value}`
  return ''
})

const navItems = computed(() => [
  { id: 'access', label: t('home.nav.access') },
  { id: 'governance', label: t('home.nav.governance') },
  { id: 'security', label: t('home.nav.security') },
  { id: 'onboarding', label: t('home.nav.onboarding') },
])

const facts = computed(() => [
  { id: 'levels', value: '8', unit: t('home.facts.levels.unit'), label: t('home.facts.levels.label'), desc: t('home.facts.levels.desc') },
  { id: 'periods', value: '4', unit: t('home.facts.periods.unit'), label: t('home.facts.periods.label'), desc: t('home.facts.periods.desc') },
  { id: 'protocols', value: '3', unit: t('home.facts.protocols.unit'), label: t('home.facts.protocols.label'), desc: t('home.facts.protocols.desc') },
  { id: 'key', value: '1', unit: t('home.facts.key.unit'), label: t('home.facts.key.label'), desc: t('home.facts.key.desc') },
])

const accessPoints = computed(() => [
  { id: 'protocol', title: t('home.access.points.protocol.title'), desc: t('home.access.points.protocol.desc') },
  { id: 'routing', title: t('home.access.points.routing.title'), desc: t('home.access.points.routing.desc') },
  { id: 'media', title: t('home.access.points.media.title'), desc: t('home.access.points.media.desc') },
])

const endpoints = [
  { protocol: 'OpenAI · Chat', method: 'POST', path: '/v1/chat/completions' },
  { protocol: 'OpenAI · Responses', method: 'POST', path: '/v1/responses' },
  { protocol: 'Anthropic · Messages', method: 'POST', path: '/v1/messages' },
  { protocol: 'Gemini', method: 'POST', path: '/v1beta/models/{model}:generateContent' },
  { protocol: 'Images', method: 'POST', path: '/v1/images/generations' },
  { protocol: 'Videos', method: 'POST', path: '/v1/videos/generations' },
  { protocol: 'Models', method: 'GET', path: '/v1/models' },
] as const

const governanceItems = computed<{ id: string; icon: IconName; title: string; desc: string }[]>(() => [
  { id: 'org', icon: 'users', title: t('home.governance.items.org.title'), desc: t('home.governance.items.org.desc') },
  { id: 'quota', icon: 'chartBar', title: t('home.governance.items.quota.title'), desc: t('home.governance.items.quota.desc') },
  { id: 'groups', icon: 'filter', title: t('home.governance.items.groups.title'), desc: t('home.governance.items.groups.desc') },
  { id: 'roles', icon: 'badge', title: t('home.governance.items.roles.title'), desc: t('home.governance.items.roles.desc') },
])

const quotaRows = computed(() => {
  const status = {
    normal: t('home.governance.table.status.normal'),
    warning: t('home.governance.table.status.warning'),
    unlimited: t('home.governance.table.status.unlimited'),
    disabled: t('home.governance.table.status.disabled'),
  }
  return [
    { id: 'rd', child: false, name: t('home.governance.table.rows.rd'), used: '$3,720', limit: '$6,000', percent: 62, status: 'normal', statusLabel: status.normal },
    { id: 'backend', child: true, name: t('home.governance.table.rows.backend'), used: '$1,700', limit: '$2,000', percent: 85, status: 'warning', statusLabel: status.warning },
    { id: 'algorithm', child: true, name: t('home.governance.table.rows.algorithm'), used: '$1,240', limit: t('home.governance.table.unlimited'), percent: 100, status: 'unlimited', statusLabel: status.unlimited },
    { id: 'marketing', child: false, name: t('home.governance.table.rows.marketing'), used: '$380', limit: '$2,000', percent: 19, status: 'normal', statusLabel: status.normal },
    { id: 'design', child: false, name: t('home.governance.table.rows.design'), used: '$0', limit: '$0', percent: 0, status: 'disabled', statusLabel: status.disabled },
  ]
})

const securityItems = computed<{ id: string; icon: IconName; title: string; desc: string }[]>(() => [
  { id: 'usage', icon: 'chart', title: t('home.security.items.usage.title'), desc: t('home.security.items.usage.desc') },
  { id: 'audit', icon: 'clipboard', title: t('home.security.items.audit.title'), desc: t('home.security.items.audit.desc') },
  { id: 'prompt', icon: 'document', title: t('home.security.items.prompt.title'), desc: t('home.security.items.prompt.desc') },
  { id: 'risk', icon: 'shield', title: t('home.security.items.risk.title'), desc: t('home.security.items.risk.desc') },
  { id: 'sso', icon: 'lock', title: t('home.security.items.sso.title'), desc: t('home.security.items.sso.desc') },
  { id: 'notify', icon: 'bell', title: t('home.security.items.notify.title'), desc: t('home.security.items.notify.desc') },
])

const ledgerRows = computed(() => [
  { time: '14:02:11', op: 'POST /v1/messages', actor: t('home.security.ledger.rows.call'), status: '200', meta: '$0.042', blocked: false },
  { time: '14:02:09', op: 'PATCH quota', actor: t('home.security.ledger.rows.quota'), status: 'OK', meta: '$2,000', blocked: false },
  { time: '14:01:57', op: 'POST /v1/images/generations', actor: t('home.security.ledger.rows.image'), status: '200', meta: '$0.160', blocked: false },
  {
    time: '14:01:40',
    op: 'POST /v1/chat/completions',
    actor: t('home.security.ledger.rows.blocked'),
    status: '403',
    meta: t('home.security.ledger.rows.blockedMeta'),
    blocked: true,
  },
])

const steps = computed(() => [
  { id: 'open', title: t('home.onboarding.steps.open.title'), desc: t('home.onboarding.steps.open.desc') },
  { id: 'org', title: t('home.onboarding.steps.org.title'), desc: t('home.onboarding.steps.org.desc') },
  { id: 'quota', title: t('home.onboarding.steps.quota.title'), desc: t('home.onboarding.steps.quota.desc') },
  { id: 'key', title: t('home.onboarding.steps.key.title'), desc: t('home.onboarding.steps.key.desc') },
])

function prefersReducedMotion(): boolean {
  return typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches === true
}

function scrollToSection(id: string) {
  const target = document.getElementById(id)
  if (!target) return
  target.scrollIntoView({ behavior: prefersReducedMotion() ? 'auto' : 'smooth', block: 'start' })
  if (id === 'tl-main') target.focus({ preventScroll: true })
}

function scrollToTop() {
  window.scrollTo({ top: 0, behavior: prefersReducedMotion() ? 'auto' : 'smooth' })
}

function copyBaseUrl() {
  void copyToClipboard(props.apiBaseUrl, t('home.access.panel.copied'))
}

function onScroll() {
  scrolled.value = window.scrollY > 8
}

let observer: IntersectionObserver | null = null

onMounted(() => {
  onScroll()
  window.addEventListener('scroll', onScroll, { passive: true })

  const root = rootRef.value
  if (!root) return
  const targets = Array.from(root.querySelectorAll<HTMLElement>('.tl-reveal'))
  if (typeof IntersectionObserver === 'undefined' || prefersReducedMotion()) {
    targets.forEach((el) => el.classList.add('is-visible'))
    return
  }
  root.classList.add('tl-js-reveal')
  observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue
        entry.target.classList.add('is-visible')
        observer?.unobserve(entry.target)
      }
    },
    { rootMargin: '0px 0px -8% 0px', threshold: 0.12 },
  )
  targets.forEach((el) => observer?.observe(el))
})

onBeforeUnmount(() => {
  window.removeEventListener('scroll', onScroll)
  observer?.disconnect()
  observer = null
})
</script>

<style scoped>
/* ================= Tokens ================= */
.tl-home {
  --tl-paper: #f2efe6;
  --tl-paper-2: #e9e5d8;
  --tl-surface: #f8f6f0;
  --tl-ink: #10221d;
  --tl-ink-soft: #3a4c46;
  --tl-muted: #66766f;
  --tl-rule: rgba(16, 34, 29, 0.12);
  --tl-rule-strong: rgba(16, 34, 29, 0.24);
  --tl-contour: rgba(16, 34, 29, 0.2);
  --tl-contour-strong: rgba(16, 34, 29, 0.46);
  --tl-signal: #0b7565;
  --tl-signal-soft: rgba(11, 117, 101, 0.1);
  --tl-marker: #dc5418;
  --tl-marker-soft: rgba(220, 84, 24, 0.12);
  --tl-band: #0f1f1b;
  --tl-band-2: #162a25;
  --tl-band-ink: #ece8dc;
  --tl-band-muted: #93a39d;
  --tl-band-rule: rgba(236, 232, 220, 0.12);
  --tl-band-signal: #4fd1b6;
  --tl-font-display: 'Bricolage Grotesque Variable', system-ui, -apple-system, 'PingFang SC', 'Hiragino Sans GB',
    'Microsoft YaHei', sans-serif;
  --tl-font-mono: 'IBM Plex Mono', ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;

  position: relative;
  min-height: 100vh;
  overflow-x: clip;
  background: var(--tl-paper);
  color: var(--tl-ink);
  font-feature-settings: 'palt';
}

.dark .tl-home {
  --tl-paper: #0a1311;
  --tl-paper-2: #0f1b18;
  --tl-surface: #12201c;
  --tl-ink: #ebe7dc;
  --tl-ink-soft: #bcc6c0;
  --tl-muted: #82938c;
  --tl-rule: rgba(235, 231, 220, 0.1);
  --tl-rule-strong: rgba(235, 231, 220, 0.2);
  --tl-contour: rgba(235, 231, 220, 0.15);
  --tl-contour-strong: rgba(235, 231, 220, 0.36);
  --tl-signal: #44cdb1;
  --tl-signal-soft: rgba(68, 205, 177, 0.12);
  --tl-marker: #ff7b45;
  --tl-marker-soft: rgba(255, 123, 69, 0.14);
  --tl-band: #12201c;
  --tl-band-2: #182a25;
}

.tl-grain {
  pointer-events: none;
  position: absolute;
  inset: 0;
  z-index: 0;
  opacity: 0.35;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='160' height='160'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='.9' numOctaves='2' stitchTiles='stitch'/%3E%3CfeColorMatrix values='0 0 0 0 0.06 0 0 0 0 0.13 0 0 0 0 0.11 0 0 0 .07 0'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E");
}

.dark .tl-grain {
  opacity: 0.6;
  mix-blend-mode: screen;
}

.tl-container {
  position: relative;
  z-index: 1;
  width: 100%;
  max-width: 1240px;
  margin: 0 auto;
  padding: 0 20px;
}

@media (min-width: 640px) {
  .tl-container {
    padding: 0 32px;
  }
}

.tl-mono {
  font-family: var(--tl-font-mono);
}

.tl-skip {
  position: absolute;
  left: 16px;
  top: -60px;
  z-index: 60;
  padding: 8px 14px;
  background: var(--tl-ink);
  color: var(--tl-paper);
  font-size: 14px;
  transition: top 0.2s ease;
}

.tl-skip:focus {
  top: 12px;
}

#tl-main:focus {
  outline: none;
}

.tl-home :is(a, button):focus-visible {
  outline: 2px solid var(--tl-signal);
  outline-offset: 3px;
}

/* ================= Header ================= */
.tl-header {
  position: sticky;
  top: 0;
  z-index: 40;
  border-bottom: 1px solid transparent;
  transition:
    background-color 0.3s ease,
    border-color 0.3s ease;
}

.tl-header.is-scrolled {
  background: color-mix(in srgb, var(--tl-paper) 94%, transparent);
  border-bottom-color: var(--tl-rule);
  backdrop-filter: blur(14px) saturate(1.2);
}

.tl-header-inner {
  display: flex;
  align-items: center;
  gap: 24px;
  height: 68px;
}

.tl-brand {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  color: var(--tl-ink);
  text-decoration: none;
}

.tl-brand-logo {
  width: 30px;
  height: 30px;
  flex: none;
  border-radius: 8px;
  object-fit: contain;
}

.tl-brand-glyph {
  width: 30px;
  height: 30px;
  flex: none;
}

.tl-brand-glyph path {
  fill: none;
  stroke: currentColor;
  stroke-width: 1.6;
}

.tl-brand-glyph path:first-child {
  stroke-width: 1.2;
  opacity: 0.55;
}

.tl-brand-peak {
  fill: var(--tl-marker);
}

.tl-wordmark {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--tl-font-display);
  font-size: 19px;
  font-weight: 680;
  font-stretch: 80%;
  letter-spacing: 0.16em;
  text-transform: uppercase;
}

.tl-nav {
  display: none;
  align-items: center;
  gap: 4px;
  margin-left: 12px;
}

@media (min-width: 1024px) {
  .tl-nav {
    display: flex;
  }
}

.tl-nav a {
  position: relative;
  padding: 8px 12px;
  font-size: 14px;
  color: var(--tl-ink-soft);
  text-decoration: none;
  transition: color 0.2s ease;
}

.tl-nav a::after {
  content: '';
  position: absolute;
  left: 12px;
  right: 12px;
  bottom: 3px;
  height: 1px;
  background: var(--tl-signal);
  transform: scaleX(0);
  transform-origin: left;
  transition: transform 0.25s ease;
}

.tl-nav a:hover {
  color: var(--tl-ink);
}

.tl-nav a:hover::after {
  transform: scaleX(1);
}

.tl-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-left: auto;
}

.tl-icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-width: 38px;
  height: 38px;
  padding: 0 8px;
  border-radius: 8px;
  color: var(--tl-ink-soft);
  font-size: 14px;
  text-decoration: none;
  transition:
    background-color 0.2s ease,
    color 0.2s ease;
}

.tl-icon-btn:hover {
  background: var(--tl-rule);
  color: var(--tl-ink);
}

.tl-hide-mobile {
  display: none;
}

@media (min-width: 640px) {
  .tl-hide-mobile {
    display: inline;
  }
}

/* ================= Buttons ================= */
.tl-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  height: 44px;
  padding: 0 20px;
  border: 1px solid transparent;
  border-radius: 999px;
  font-size: 15px;
  font-weight: 600;
  white-space: nowrap;
  text-decoration: none;
  transition:
    background-color 0.2s ease,
    color 0.2s ease,
    border-color 0.2s ease,
    transform 0.2s ease;
}

.tl-btn:active {
  transform: translateY(1px);
}

.tl-btn--sm {
  height: 36px;
  margin-left: 6px;
  padding: 0 16px;
  font-size: 14px;
}

.tl-btn--lg {
  height: 50px;
  padding: 0 24px;
}

.tl-btn--ink {
  background: var(--tl-ink);
  color: var(--tl-paper);
}

.tl-btn--ink:hover {
  background: var(--tl-signal);
  color: #fff;
}

.dark .tl-btn--ink:hover {
  color: var(--tl-paper);
}

.tl-btn--ghost {
  border-color: var(--tl-rule-strong);
  color: var(--tl-ink);
}

.tl-btn--ghost:hover {
  border-color: var(--tl-ink);
  background: var(--tl-surface);
}

.tl-btn--signal {
  background: var(--tl-band-signal);
  color: #062420;
}

.tl-btn--signal:hover {
  background: #7be3cd;
}

.tl-btn--outline-light {
  border-color: var(--tl-band-rule);
  color: var(--tl-band-ink);
}

.tl-btn--outline-light:hover {
  border-color: var(--tl-band-ink);
}

.tl-btn-arrow {
  transition: transform 0.2s ease;
}

.tl-btn:hover .tl-btn-arrow {
  transform: translateX(3px);
}

/* ================= Hero ================= */
.tl-hero {
  position: relative;
  padding: 40px 0 72px;
}

@media (min-width: 1024px) {
  .tl-hero {
    padding: 56px 0 96px;
  }
}

.tl-hero-grid {
  position: absolute;
  inset: -68px 0 0;
  background-image:
    linear-gradient(var(--tl-rule) 1px, transparent 1px),
    linear-gradient(90deg, var(--tl-rule) 1px, transparent 1px);
  background-size: 80px 80px;
  background-position: center top;
  -webkit-mask-image: radial-gradient(ellipse 75% 70% at 70% 40%, #000 20%, transparent 75%);
  mask-image: radial-gradient(ellipse 75% 70% at 70% 40%, #000 20%, transparent 75%);
  opacity: 0.55;
}

.tl-hero-inner {
  display: grid;
  gap: 40px;
  align-items: center;
}

@media (min-width: 1024px) {
  .tl-hero-inner {
    grid-template-columns: minmax(0, 1.02fr) minmax(0, 1fr);
    gap: 24px;
  }
}

.tl-eyebrow {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  margin: 0 0 28px;
  font-family: var(--tl-font-mono);
  font-size: 12.5px;
  font-weight: 500;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--tl-signal);
}

.tl-eyebrow-mark {
  width: 22px;
  height: 1px;
  background: currentColor;
}

.tl-eyebrow-coord {
  padding: 2px 8px;
  border: 1px solid var(--tl-rule-strong);
  border-radius: 999px;
  font-size: 11px;
  color: var(--tl-muted);
  letter-spacing: 0.06em;
}

.tl-title {
  margin: 0;
  font-family: var(--tl-font-display);
  font-size: clamp(2.5rem, 5vw, 4.3rem);
  font-weight: 760;
  font-stretch: 92%;
  line-height: 1.08;
  letter-spacing: -0.025em;
  color: var(--tl-ink);
}

.tl-title-line {
  display: block;
}

.tl-title-accent {
  position: relative;
  display: inline-block;
  color: var(--tl-signal);
  padding-bottom: 0.08em;
}

.tl-title-accent::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  bottom: -0.12em;
  height: 0.28em;
  background: var(--tl-signal);
  opacity: 0.55;
  -webkit-mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 28 12'%3E%3Cpath d='M0 4Q7 0 14 4T28 4M0 10Q7 6 14 10T28 10' fill='none' stroke='%23000' stroke-width='1.4'/%3E%3C/svg%3E")
    repeat-x left center / 0.62em 100%;
  mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 28 12'%3E%3Cpath d='M0 4Q7 0 14 4T28 4M0 10Q7 6 14 10T28 10' fill='none' stroke='%23000' stroke-width='1.4'/%3E%3C/svg%3E")
    repeat-x left center / 0.62em 100%;
  transform-origin: left;
  transform: scaleX(0);
  animation: tl-underline 1.1s cubic-bezier(0.65, 0.05, 0.25, 1) 0.9s forwards;
}

.tl-lede {
  max-width: 34em;
  margin: 28px 0 0;
  font-size: 17px;
  line-height: 1.8;
  color: var(--tl-ink-soft);
}

.tl-hero-ctas {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 36px;
}

.tl-hero-note {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 22px 0 0;
  font-size: 13px;
  color: var(--tl-muted);
}

.tl-hero-map {
  position: relative;
  max-width: 620px;
  width: 100%;
  justify-self: center;
}

@media (min-width: 1024px) {
  .tl-hero-map {
    max-width: none;
    width: 112%;
    margin-right: -12%;
  }
}

.tl-rise {
  opacity: 0;
  transform: translateY(16px);
  animation: tl-rise 0.85s cubic-bezier(0.2, 0.8, 0.2, 1) forwards;
  animation-delay: calc(var(--d) * 90ms + 120ms);
}

/* ================= Facts ================= */
.tl-facts {
  position: relative;
  border-top: 1px solid var(--tl-rule-strong);
  border-bottom: 1px solid var(--tl-rule);
  background: color-mix(in srgb, var(--tl-surface) 60%, transparent);
}

.tl-facts-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  margin: 0;
}

@media (min-width: 1024px) {
  .tl-facts-grid {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

.tl-fact {
  display: flex;
  flex-direction: column;
  padding: 28px 20px 30px;
  border-left: 1px solid var(--tl-rule);
}

.tl-fact:nth-child(odd) {
  border-left: 0;
  padding-left: 0;
}

@media (min-width: 1024px) {
  .tl-fact,
  .tl-fact:nth-child(odd) {
    padding: 36px 28px 38px;
    border-left: 1px solid var(--tl-rule);
  }

  .tl-fact:first-child {
    padding-left: 0;
    border-left: 0;
  }
}

.tl-fact-label {
  order: -1;
  font-family: var(--tl-font-mono);
  font-size: 12px;
  letter-spacing: 0.04em;
  color: var(--tl-muted);
}

.tl-fact-value {
  display: flex;
  align-items: baseline;
  gap: 8px;
  margin: 10px 0 0;
}

.tl-fact-num {
  font-family: var(--tl-font-display);
  font-size: clamp(3rem, 6vw, 4.5rem);
  font-weight: 640;
  font-stretch: 76%;
  line-height: 1;
  letter-spacing: -0.03em;
  color: var(--tl-ink);
}

.tl-fact-unit {
  font-size: 15px;
  font-weight: 600;
  color: var(--tl-signal);
}

.tl-fact-desc {
  margin: 10px 0 0;
  font-size: 13.5px;
  line-height: 1.6;
  color: var(--tl-ink-soft);
}

/* ================= Sections ================= */
.tl-section {
  position: relative;
  padding: 88px 0;
  scroll-margin-top: 56px;
}

@media (min-width: 1024px) {
  .tl-section {
    padding: 128px 0;
  }
}

.tl-section--tint {
  background: var(--tl-paper-2);
  border-top: 1px solid var(--tl-rule);
  border-bottom: 1px solid var(--tl-rule);
}

.tl-section-head {
  display: grid;
  gap: 16px 32px;
  margin-bottom: 56px;
}

@media (min-width: 1024px) {
  .tl-section-head {
    grid-template-columns: 96px minmax(0, 1.1fr) minmax(0, 1fr);
    align-items: end;
    margin-bottom: 72px;
  }

  .tl-section-head--compact {
    grid-template-columns: 96px minmax(0, 1fr);
  }
}

.tl-section-index {
  font-family: var(--tl-font-display);
  font-size: 56px;
  font-weight: 300;
  font-stretch: 75%;
  line-height: 0.9;
  color: transparent;
  -webkit-text-stroke: 1px var(--tl-contour-strong);
}

.tl-kicker {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 0 0 14px;
  font-family: var(--tl-font-mono);
  font-size: 12px;
  font-weight: 500;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--tl-signal);
}

.tl-kicker::before {
  content: '';
  width: 7px;
  height: 7px;
  border: 1.5px solid var(--tl-marker);
  border-radius: 999px;
}

.tl-h2 {
  margin: 0;
  font-family: var(--tl-font-display);
  font-size: clamp(1.9rem, 3.4vw, 2.75rem);
  font-weight: 720;
  font-stretch: 92%;
  line-height: 1.18;
  letter-spacing: -0.02em;
  text-wrap: balance;
}

.tl-section-desc {
  margin: 0;
  max-width: 36em;
  font-size: 15.5px;
  line-height: 1.8;
  color: var(--tl-ink-soft);
}

.tl-h3 {
  margin: 0;
  font-size: 17px;
  font-weight: 650;
  line-height: 1.4;
  color: var(--tl-ink);
}

.tl-body {
  margin: 8px 0 0;
  font-size: 14.5px;
  line-height: 1.75;
  color: var(--tl-ink-soft);
}

.tl-split {
  display: grid;
  gap: 48px;
  align-items: start;
}

@media (min-width: 1024px) {
  .tl-split {
    grid-template-columns: minmax(0, 0.9fr) minmax(0, 1.1fr);
    gap: 72px;
  }

  .tl-split--reverse {
    grid-template-columns: minmax(0, 1.1fr) minmax(0, 0.9fr);
  }
}

/* ---------- Access points ---------- */
.tl-points {
  margin: 0;
  padding: 0;
  list-style: none;
  border-top: 1px solid var(--tl-rule-strong);
}

.tl-point {
  display: grid;
  grid-template-columns: 48px minmax(0, 1fr);
  gap: 8px;
  padding: 28px 0;
  border-bottom: 1px solid var(--tl-rule);
}

.tl-point-index {
  padding-top: 3px;
  font-family: var(--tl-font-mono);
  font-size: 13px;
  color: var(--tl-marker);
}

/* ---------- Endpoint panel ---------- */
.tl-panel {
  overflow: hidden;
  border-radius: 14px;
  background: var(--tl-band);
  color: var(--tl-band-ink);
  box-shadow:
    0 1px 0 rgba(255, 255, 255, 0.04) inset,
    0 30px 60px -30px rgba(10, 30, 25, 0.55);
}

.tl-panel-head {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 14px 18px;
  border-bottom: 1px solid var(--tl-band-rule);
  font-family: var(--tl-font-mono);
  font-size: 12px;
  letter-spacing: 0.06em;
  color: var(--tl-band-muted);
}

.tl-panel-lights {
  display: inline-flex;
  gap: 6px;
}

.tl-panel-lights i {
  width: 9px;
  height: 9px;
  border-radius: 999px;
  border: 1px solid var(--tl-band-rule);
}

.tl-panel-lights i:first-child {
  background: var(--tl-marker);
  border-color: transparent;
}

.tl-panel-base {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px 14px;
  padding: 18px;
  border-bottom: 1px solid var(--tl-band-rule);
  background: var(--tl-band-2);
}

.tl-panel-label {
  font-family: var(--tl-font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--tl-band-muted);
}

.tl-panel-url {
  flex: 1 1 auto;
  min-width: 0;
  overflow-wrap: anywhere;
  font-family: var(--tl-font-mono);
  font-size: 14px;
  color: var(--tl-band-signal);
}

.tl-panel-copy {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  padding: 0 12px;
  border: 1px solid var(--tl-band-rule);
  border-radius: 999px;
  font-size: 12.5px;
  color: var(--tl-band-ink);
  transition: border-color 0.2s ease;
}

.tl-panel-copy:hover {
  border-color: var(--tl-band-signal);
}

.tl-panel-scroll {
  overflow-x: auto;
}

.tl-endpoints {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.tl-endpoints th {
  padding: 12px 18px 8px;
  font-family: var(--tl-font-mono);
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0.08em;
  text-align: left;
  text-transform: uppercase;
  color: var(--tl-band-muted);
}

.tl-endpoints td {
  padding: 10px 18px;
  border-top: 1px solid var(--tl-band-rule);
  white-space: nowrap;
}

.tl-endpoints tbody tr {
  transition: background-color 0.15s ease;
}

.tl-endpoints tbody tr:hover {
  background: rgba(255, 255, 255, 0.03);
}

.tl-endpoint-protocol {
  color: var(--tl-band-ink);
}

.tl-endpoint-method {
  display: inline-block;
  width: 42px;
  font-family: var(--tl-font-mono);
  font-size: 11px;
  font-weight: 500;
  color: var(--tl-marker);
}

.tl-endpoint-method.is-get {
  color: var(--tl-band-signal);
}

.tl-endpoint-path {
  font-family: var(--tl-font-mono);
  font-size: 12.5px;
  color: var(--tl-band-ink);
  opacity: 0.88;
}

.tl-panel-note {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin: 0;
  padding: 14px 18px 16px;
  border-top: 1px solid var(--tl-band-rule);
  font-size: 12.5px;
  line-height: 1.6;
  color: var(--tl-band-muted);
}

.tl-panel-note :deep(svg) {
  flex: none;
  margin-top: 3px;
}

/* ---------- Quota sheet ---------- */
.tl-sheet {
  border: 1px solid var(--tl-rule-strong);
  border-radius: 14px;
  background: var(--tl-surface);
  box-shadow: 0 24px 48px -32px rgba(16, 34, 29, 0.35);
}

.tl-sheet-head {
  display: flex;
  justify-content: space-between;
  padding: 16px 20px;
  border-bottom: 1px solid var(--tl-rule);
  font-size: 14px;
  font-weight: 650;
}

.tl-sheet-head .tl-mono {
  font-size: 11px;
  font-weight: 500;
  color: var(--tl-muted);
}

.tl-sheet-cols {
  display: flex;
  justify-content: space-between;
  padding: 10px 20px;
  font-family: var(--tl-font-mono);
  font-size: 11px;
  letter-spacing: 0.04em;
  color: var(--tl-muted);
  border-bottom: 1px solid var(--tl-rule);
}

.tl-sheet-rows {
  margin: 0;
  padding: 0;
  list-style: none;
}

.tl-row {
  display: grid;
  grid-template-columns: minmax(84px, 0.9fr) minmax(60px, 1.2fr) auto;
  grid-template-areas:
    'name bar chip'
    'name num num';
  align-items: center;
  gap: 4px 14px;
  padding: 14px 20px;
  border-bottom: 1px solid var(--tl-rule);
}

@media (min-width: 640px) {
  .tl-row {
    grid-template-columns: minmax(96px, 0.9fr) minmax(80px, 1.3fr) 128px 76px;
    grid-template-areas: 'name bar num chip';
  }
}

.tl-row:last-child {
  border-bottom: 0;
}

.tl-row-name {
  grid-area: name;
  position: relative;
  font-size: 14px;
  font-weight: 600;
}

.tl-row.is-child .tl-row-name {
  padding-left: 20px;
  font-weight: 500;
  color: var(--tl-ink-soft);
}

.tl-row.is-child .tl-row-name::before {
  content: '';
  position: absolute;
  left: 4px;
  top: -6px;
  width: 9px;
  height: 14px;
  border-left: 1px solid var(--tl-rule-strong);
  border-bottom: 1px solid var(--tl-rule-strong);
}

.tl-row-bar {
  grid-area: bar;
  position: relative;
  height: 6px;
  border-radius: 999px;
  background: var(--tl-rule);
  overflow: hidden;
}

.tl-row-fill {
  position: absolute;
  inset: 0 auto 0 0;
  width: 0;
  border-radius: inherit;
  background: var(--tl-signal);
  transition: width 1.2s cubic-bezier(0.2, 0.8, 0.2, 1) 0.25s;
}

.tl-row-fill.is-warning {
  background: var(--tl-marker);
}

.tl-row-fill.is-unlimited {
  background: repeating-linear-gradient(
    -45deg,
    var(--tl-signal-soft) 0 4px,
    color-mix(in srgb, var(--tl-signal) 45%, transparent) 4px 6px
  );
}

.tl-row-num {
  grid-area: num;
  font-family: var(--tl-font-mono);
  font-size: 12px;
  color: var(--tl-ink-soft);
  text-align: right;
  white-space: nowrap;
}

.tl-chip {
  grid-area: chip;
  justify-self: end;
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 11.5px;
  font-weight: 600;
  white-space: nowrap;
}

.tl-chip.is-normal {
  background: var(--tl-signal-soft);
  color: var(--tl-signal);
}

.tl-chip.is-warning {
  background: var(--tl-marker-soft);
  color: var(--tl-marker);
}

.tl-chip.is-unlimited {
  border: 1px solid var(--tl-rule-strong);
  color: var(--tl-ink-soft);
}

.tl-chip.is-disabled {
  background: var(--tl-rule);
  color: var(--tl-muted);
  text-decoration: line-through;
}

.tl-sheet-foot {
  margin: 0;
  padding: 12px 20px 14px;
  border-top: 1px dashed var(--tl-rule-strong);
  font-size: 12px;
  color: var(--tl-muted);
}

/* ---------- Governance features ---------- */
.tl-feature-grid {
  display: grid;
  gap: 1px;
  background: var(--tl-rule);
  border: 1px solid var(--tl-rule);
}

@media (min-width: 640px) {
  .tl-feature-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

.tl-feature {
  padding: 26px 24px 28px;
  background: var(--tl-paper-2);
  transition: background-color 0.25s ease;
}

.tl-feature:hover {
  background: var(--tl-surface);
}

.tl-feature-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  margin-bottom: 18px;
  border: 1px solid var(--tl-rule-strong);
  border-radius: 10px;
  color: var(--tl-signal);
}

/* ================= Security band ================= */
.tl-band {
  overflow: hidden;
  background: var(--tl-band);
  color: var(--tl-band-ink);
  --tl-ink: var(--tl-band-ink);
  --tl-ink-soft: #b8c4bf;
  --tl-muted: var(--tl-band-muted);
  --tl-rule: var(--tl-band-rule);
  --tl-rule-strong: rgba(236, 232, 220, 0.22);
  --tl-contour-strong: rgba(236, 232, 220, 0.3);
  --tl-signal: var(--tl-band-signal);
}

.tl-band-contours {
  position: absolute;
  inset: 0;
  opacity: 0.5;
  background: repeating-radial-gradient(
    ellipse 120% 90% at 110% 0%,
    transparent 0 46px,
    rgba(236, 232, 220, 0.07) 46px 47px
  );
  -webkit-mask-image: linear-gradient(to bottom, #000, transparent 85%);
  mask-image: linear-gradient(to bottom, #000, transparent 85%);
}

.tl-security {
  display: grid;
  gap: 48px;
}

@media (min-width: 1024px) {
  .tl-security {
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: 64px;
    align-items: start;
  }
}

.tl-security-grid {
  display: grid;
  gap: 32px 28px;
  margin: 0;
  padding: 0;
  list-style: none;
}

@media (min-width: 640px) {
  .tl-security-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

.tl-security-item {
  display: grid;
  grid-template-columns: 24px minmax(0, 1fr);
  gap: 14px;
}

.tl-security-icon {
  margin-top: 2px;
  color: var(--tl-band-signal);
}

.tl-ledger {
  border: 1px solid var(--tl-band-rule);
  border-radius: 14px;
  background: rgba(0, 0, 0, 0.18);
  font-size: 13px;
}

.tl-ledger-head {
  display: flex;
  justify-content: space-between;
  padding: 14px 18px;
  border-bottom: 1px solid var(--tl-band-rule);
  font-family: var(--tl-font-mono);
  font-size: 12px;
  letter-spacing: 0.06em;
  color: var(--tl-band-muted);
}

.tl-ledger-live {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--tl-band-signal);
}

.tl-ledger-live i {
  width: 6px;
  height: 6px;
  border-radius: 999px;
  background: currentColor;
  animation: tl-blink 1.6s ease-in-out infinite;
}

.tl-ledger-rows {
  margin: 0;
  padding: 6px 0;
  list-style: none;
}

.tl-ledger-row {
  display: grid;
  grid-template-columns: 70px minmax(0, 1fr) auto;
  grid-template-areas:
    'time op status'
    'time actor meta';
  gap: 4px 12px;
  padding: 12px 18px;
  border-bottom: 1px dashed var(--tl-band-rule);
}

.tl-ledger-row:last-child {
  border-bottom: 0;
}

.tl-ledger-time {
  grid-area: time;
  font-family: var(--tl-font-mono);
  font-size: 11.5px;
  color: var(--tl-band-muted);
}

.tl-ledger-op {
  grid-area: op;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--tl-font-mono);
  font-size: 12.5px;
  color: var(--tl-band-ink);
}

.tl-ledger-actor {
  grid-area: actor;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12.5px;
  color: var(--tl-band-muted);
}

.tl-ledger-status {
  grid-area: status;
  justify-self: end;
  font-family: var(--tl-font-mono);
  font-size: 12px;
  color: var(--tl-band-signal);
}

.tl-ledger-meta {
  grid-area: meta;
  justify-self: end;
  font-family: var(--tl-font-mono);
  font-size: 12px;
  color: var(--tl-band-muted);
}

.tl-ledger-row.is-blocked {
  background: linear-gradient(90deg, rgba(255, 123, 69, 0.1), transparent 70%);
  box-shadow: inset 2px 0 0 var(--tl-marker);
}

.tl-ledger-row.is-blocked .tl-ledger-status,
.tl-ledger-row.is-blocked .tl-ledger-meta {
  color: #ff8a5c;
}

/* ================= Steps ================= */
.tl-steps {
  position: relative;
  display: grid;
  gap: 36px;
  margin: 0;
  padding: 0;
  list-style: none;
}

@media (min-width: 768px) {
  .tl-steps {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (min-width: 1024px) {
  .tl-steps {
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 28px;
  }

  .tl-steps::before {
    content: '';
    position: absolute;
    left: 28px;
    right: calc((100% - 84px) / 4 - 28px);
    top: 28px;
    border-top: 1px dashed var(--tl-rule-strong);
  }
}

.tl-step {
  position: relative;
}

.tl-step-node {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 56px;
  height: 56px;
  margin-bottom: 22px;
  border: 1px solid var(--tl-rule-strong);
  border-radius: 999px;
  background: var(--tl-paper);
  box-shadow:
    0 0 0 6px var(--tl-paper),
    0 0 0 7px var(--tl-rule);
}

.tl-step:last-child .tl-step-node {
  border-color: var(--tl-marker);
}

.tl-step-num {
  font-family: var(--tl-font-display);
  font-size: 20px;
  font-weight: 620;
  font-stretch: 80%;
  color: var(--tl-ink);
}

.tl-step:last-child .tl-step-num {
  color: var(--tl-marker);
}

/* ================= CTA ================= */
.tl-cta {
  padding: 0 0 96px;
}

.tl-cta-card {
  position: relative;
  display: grid;
  gap: 32px;
  overflow: hidden;
  padding: 48px 28px;
  border-radius: 20px;
  background: var(--tl-band);
  color: var(--tl-band-ink);
}

@media (min-width: 1024px) {
  .tl-cta-card {
    grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr);
    align-items: end;
    padding: 72px 64px;
  }
}

.tl-cta-contours {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
}

.tl-cta-contours ellipse {
  fill: none;
  stroke: rgba(236, 232, 220, 0.09);
  stroke-width: 1;
}

.tl-cta-copy,
.tl-cta-actions {
  position: relative;
}

.tl-cta-title {
  color: var(--tl-band-ink);
}

.tl-cta-desc {
  max-width: 32em;
  margin: 18px 0 0;
  font-size: 15.5px;
  line-height: 1.8;
  color: var(--tl-band-muted);
}

.tl-cta-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
}

@media (min-width: 1024px) {
  .tl-cta-actions {
    justify-content: flex-end;
  }
}

.tl-cta-contact {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 6px 12px;
  width: 100%;
  margin: 8px 0 0;
  font-size: 14px;
  color: var(--tl-band-ink);
  overflow-wrap: anywhere;
}

@media (min-width: 1024px) {
  .tl-cta-contact {
    justify-content: flex-end;
  }
}

.tl-cta-contact-label {
  font-family: var(--tl-font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--tl-band-muted);
}

.tl-cta-contact a {
  color: var(--tl-band-signal);
  text-decoration: underline;
  text-underline-offset: 3px;
}

/* ================= Footer ================= */
.tl-footer {
  position: relative;
  border-top: 1px solid var(--tl-rule-strong);
  padding: 56px 0 28px;
}

.tl-footer-top {
  display: grid;
  gap: 40px;
}

@media (min-width: 768px) {
  .tl-footer-top {
    grid-template-columns: minmax(0, 1fr) auto;
  }
}

.tl-footer-brand p {
  margin: 10px 0 0;
  font-size: 14px;
  color: var(--tl-muted);
}

.tl-footer-cols {
  display: grid;
  grid-template-columns: repeat(2, minmax(120px, auto));
  gap: 48px;
}

.tl-footer-title {
  margin: 0 0 14px;
  font-family: var(--tl-font-mono);
  font-size: 11px;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--tl-muted);
}

.tl-footer-cols ul {
  display: grid;
  gap: 10px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.tl-footer-cols a {
  font-size: 14px;
  color: var(--tl-ink-soft);
  text-decoration: none;
  transition: color 0.2s ease;
}

.tl-footer-cols a:hover {
  color: var(--tl-signal);
}

.tl-footer-bottom {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 12px;
  margin-top: 48px;
  padding-top: 20px;
  border-top: 1px solid var(--tl-rule);
  font-size: 13px;
  color: var(--tl-muted);
}

.tl-footer-bottom .tl-mono {
  font-size: 11px;
  letter-spacing: 0.08em;
}

/* ================= Scroll reveal ================= */
.tl-js-reveal .tl-reveal {
  opacity: 0;
  transform: translateY(18px);
  transition:
    opacity 0.8s cubic-bezier(0.2, 0.8, 0.2, 1),
    transform 0.8s cubic-bezier(0.2, 0.8, 0.2, 1);
  transition-delay: calc(var(--d, 0) * 80ms);
}

.tl-js-reveal .tl-reveal.is-visible {
  opacity: 1;
  transform: none;
}

.tl-reveal.is-visible .tl-row-fill {
  width: var(--w);
}

/* ================= Keyframes ================= */
@keyframes tl-rise {
  to {
    opacity: 1;
    transform: none;
  }
}

@keyframes tl-underline {
  to {
    transform: scaleX(1);
  }
}

@keyframes tl-blink {
  50% {
    opacity: 0.25;
  }
}

@media (prefers-reduced-motion: reduce) {
  .tl-rise,
  .tl-title-accent::after {
    opacity: 1;
    transform: none;
    animation: none;
  }

  .tl-row-fill {
    transition: none;
  }

  .tl-ledger-live i {
    animation: none;
  }
}
</style>
