<script setup lang="ts">
import { ref } from "vue";
import { useData, withBase } from "vitepress";
import HomeIcon from "./HomeIcon.vue";

const { frontmatter } = useData();
const selectedScene = ref("community");
const scenes = [
  { id: "community", label: "社区交流", icon: "chat" },
  { id: "knowledge", label: "知识检索", icon: "search" },
  { id: "extensions", label: "开放扩展", icon: "puzzle" },
];
const guides = [
  {
    number: "01",
    icon: "book",
    title: "第一次认识 CampusOS",
    text: "从项目介绍开始，找到适合自己的使用路径。",
    href: "/guide/introduction",
  },
  {
    number: "02",
    icon: "terminal",
    title: "搭建你的校园空间",
    text: "准备配置，通过 Docker 启动开发环境。",
    href: "/deployment/docker-development",
  },
  {
    number: "03",
    icon: "search",
    title: "让资料成为可用的知识",
    text: "了解公开检索、私人知识与有据问答。",
    href: "/guide/public-knowledge-search",
  },
  {
    number: "04",
    icon: "puzzle",
    title: "带来一个新的可能",
    text: "从插件合同、SDK 和独立发布流程开始。",
    href: "/plugins/overview",
  },
];
const manifest = `{
  "api_version": "campusos.plugin/v5",
  "publisher": "campusos",
  "key": "pdf-viewer",
  "runtime": "none"
}`;

function moveScene(event: KeyboardEvent) {
  const current = scenes.findIndex((scene) => scene.id === selectedScene.value);
  let next = current;
  if (event.key === "ArrowRight") next = (current + 1) % scenes.length;
  else if (event.key === "ArrowLeft")
    next = (current + scenes.length - 1) % scenes.length;
  else if (event.key === "Home") next = 0;
  else if (event.key === "End") next = scenes.length - 1;
  else return;
  event.preventDefault();
  selectedScene.value = scenes[next].id;
  (event.currentTarget as HTMLElement)
    .querySelector<HTMLButtonElement>(`#scene-${scenes[next].id}`)
    ?.focus();
}
</script>

<template>
  <div class="campus-home">
    <section class="home-hero home-container" aria-labelledby="home-title">
      <div class="home-hero-copy">
        <p class="home-eyebrow">
          <span class="home-signal"></span> 为校园而生的开放平台
        </p>
        <h1 id="home-title">连接校园里的<br /><span>每一种可能。</span></h1>
        <p class="home-lead">
          让交流有回响，让知识有来处。<br
            class="home-desktop-break"
          />把社区、个人内容与知识检索，放进同一个校园空间。
        </p>
        <div class="home-actions">
          <a
            class="home-button home-button-primary"
            :href="withBase('/guide/getting-started')"
            >开始使用 <HomeIcon
          /></a>
          <a class="home-button home-button-secondary" href="#experience"
            >探索产品 <HomeIcon name="diagonal"
          /></a>
        </div>
        <div class="home-hero-note">
          <span class="home-note-line"></span> 开源共建
          <span aria-hidden="true">/</span> 模块化设计
          <span aria-hidden="true">/</span> 自主部署
        </div>
      </div>
      <div class="home-showcase">
        <div
          class="home-scene-tabs"
          role="tablist"
          aria-label="产品场景"
          @keydown="moveScene"
        >
          <button
            v-for="scene in scenes"
            :id="`scene-${scene.id}`"
            :key="scene.id"
            type="button"
            role="tab"
            :aria-selected="selectedScene === scene.id"
            :aria-controls="`panel-${scene.id}`"
            :tabindex="selectedScene === scene.id ? 0 : -1"
            @click="selectedScene = scene.id"
          >
            <HomeIcon :name="scene.icon" />{{ scene.label }}
          </button>
        </div>
        <div class="home-scene-window">
          <div class="home-window-bar">
            <span class="home-window-mark">C<span>·</span></span
            ><strong>CampusOS</strong
            ><span class="home-window-dots" aria-hidden="true">···</span>
          </div>
          <div
            v-for="scene in scenes"
            v-show="selectedScene === scene.id"
            :id="`panel-${scene.id}`"
            :key="scene.id"
            role="tabpanel"
            :aria-labelledby="`scene-${scene.id}`"
            tabindex="0"
            class="home-scene-panel"
          >
            <template v-if="scene.id === 'community'">
              <div class="home-demo-greeting">
                <span class="home-demo-eyebrow">OUR CAMPUS, OUR STORIES</span>
                <h2>校园生活，正在发生。</h2>
                <p>发现同频的人，分享身边的新鲜事。</p>
                <span class="home-demo-sun" aria-hidden="true">✳</span>
              </div>
              <div class="home-demo-topics">
                <span class="is-current">全部动态</span><span>校园日常</span
                ><span>学习交流</span><span>互助与二手</span>
              </div>
              <div class="home-demo-post">
                <span class="home-avatar home-avatar-green">林</span>
                <div>
                  <div class="home-demo-post-meta">
                    校园生活 <span>刚刚</span>
                  </div>
                  <h3>这个周末，一起重新认识我们的校园</h3>
                  <p>一条散步路线、一本好书，和一些值得分享的小事。</p>
                  <div class="home-demo-post-tags">
                    <span># 校园漫步</span><span># 发现日常</span>
                  </div>
                </div>
              </div>
              <div class="home-demo-post home-demo-post-small">
                <span class="home-avatar home-avatar-ochre">陈</span>
                <div>
                  <div class="home-demo-post-meta">学习交流</div>
                  <h3>把这个学期的学习方法，整理给下一位同学</h3>
                  <p>让经验被看见，也让分享持续发生。</p>
                </div>
              </div>
              <div class="home-demo-footnote">
                <HomeIcon name="shield" /> 分享给谁，由内容的可见范围决定。
              </div>
            </template>
            <template v-else-if="scene.id === 'knowledge'">
              <div class="home-demo-greeting">
                <span class="home-demo-eyebrow">FIND THE SOURCE</span>
                <h2>让答案，有据可循。</h2>
                <p>从选定的校园资料中，找到当前可读的来源。</p>
                <HomeIcon class="home-demo-large-icon" name="search" />
              </div>
              <div class="home-demo-search">
                <HomeIcon name="search" /><span>图书馆周末的开放安排</span
                ><span class="home-demo-search-label">公开资料</span>
              </div>
              <div class="home-demo-answer">
                <span class="home-demo-answer-label"
                  >引用式问答 · 场景示意</span
                >
                <h3>先找到资料，再核对答案。</h3>
                <p>
                  答案附带可复核的来源引用。资料变化或不再公开时，重新核验当前原文。
                </p>
                <div class="home-demo-citation">
                  <HomeIcon name="file" />
                  <div>
                    <strong>图书馆开放安排</strong
                    ><span>当前来源 · 公开集合</span>
                  </div>
                  <HomeIcon name="diagonal" />
                </div>
              </div>
              <div class="home-demo-footnote">
                <HomeIcon name="shield" /> 私人资料逐项选择，模型发送另行确认。
              </div>
            </template>
            <template v-else>
              <div class="home-demo-greeting">
                <span class="home-demo-eyebrow">BUILT TO GROW</span>
                <h2>为新的想法，留一个位置。</h2>
                <p>独立发布扩展，让校园空间持续生长。</p>
                <HomeIcon class="home-demo-large-icon" name="puzzle" />
              </div>
              <div class="home-demo-extension">
                <span class="home-demo-extension-icon"
                  ><HomeIcon name="file"
                /></span>
                <div>
                  <h3>PDF Viewer</h3>
                  <p>在获准阅读的范围内，预览一份资料。</p>
                </div>
                <span class="home-demo-chip">隔离 UI</span>
              </div>
              <div class="home-demo-extension">
                <span
                  class="home-demo-extension-icon home-demo-extension-icon-sand"
                  ><HomeIcon name="layers"
                /></span>
                <div>
                  <h3>主题与空间风格</h3>
                  <p>用资源包，表达校园的不同气质。</p>
                </div>
                <span class="home-demo-chip">资源包</span>
              </div>
              <div class="home-demo-extension-flow">
                <span>独立发布</span><HomeIcon /><span>审核授权</span
                ><HomeIcon /><span>受控使用</span>
              </div>
              <div class="home-demo-footnote">
                <HomeIcon name="shield" /> 安装扩展，不等于授予全部数据权限。
              </div>
            </template>
          </div>
        </div>
        <p class="home-showcase-caption">
          <span></span> 产品场景示意，非线上业务数据
        </p>
      </div>
    </section>

    <div class="home-principles home-container">
      <p>一个空间，连接校园的不同日常。</p>
      <div>
        <span>交流与互助</span><span>个人内容</span><span>校园知识</span
        ><span>开放扩展</span>
      </div>
    </div>

    <section
      id="experience"
      class="home-experience home-container"
      aria-labelledby="experience-title"
    >
      <div class="home-section-heading">
        <p class="home-eyebrow">围绕真实的校园生活</p>
        <h2 id="experience-title">从一次分享，<br />到不断积累的价值。</h2>
        <p>把分散的讨论、资料与经验，变成可以交流、沉淀和再次发现的内容。</p>
      </div>
      <div class="home-experience-grid">
        <article class="home-community-story">
          <div class="home-story-icon"><HomeIcon name="chat" /></div>
          <span class="home-story-number">01 / 连接</span>
          <h3>让校园里的声音，<br />找到彼此。</h3>
          <p>
            用版块和标签组织讨论，以互助、二手和通知连接日常。让一条帖子，成为一次真实交流的开始。
          </p>
          <a :href="withBase('/guide/structured-image-text')"
            >了解社区与内容 <HomeIcon name="diagonal"
          /></a>
          <div class="home-story-tags" aria-hidden="true">
            <span>学习交流</span><span>校园互助</span><span>二手流转</span
            ><span>兴趣日常</span>
          </div>
        </article>
        <div class="home-story-stack">
          <article class="home-story-row">
            <span class="home-story-row-icon"><HomeIcon name="file" /></span>
            <div>
              <span class="home-story-number">02 / 沉淀</span>
              <h3>让自己的内容，有一个家。</h3>
              <p>
                用个人空间、文档、图文文章和附件整理日常。文件归属与访问范围，由服务端统一管理。
              </p>
              <a :href="withBase('/guide/web-ui-storage-notifications')"
                >探索个人空间 <HomeIcon name="diagonal"
              /></a>
            </div>
          </article>
          <article class="home-story-row">
            <span class="home-story-row-icon"><HomeIcon name="search" /></span>
            <div>
              <span class="home-story-number">03 / 发现</span>
              <h3>让积累的知识，再次被找到。</h3>
              <p>
                搜索已选入的公开资料，或逐项建立自己的私人知识。启用受管模型后，用当前来源进行有据问答。
              </p>
              <a :href="withBase('/guide/cited-rag')"
                >了解知识与问答 <HomeIcon name="diagonal"
              /></a>
            </div>
          </article>
        </div>
      </div>
    </section>

    <section
      id="extensions"
      class="home-extension-section"
      aria-labelledby="extension-title"
    >
      <div class="home-container home-extension-grid">
        <div class="home-extension-copy">
          <p class="home-eyebrow">可生长的校园平台</p>
          <h2 id="extension-title">开放扩展，<br />也有清晰边界。</h2>
          <p>
            从一个预览插件，到新的知识能力。扩展独立发布，安装、授权、配置与运行各有明确的责任。
          </p>
          <a class="home-text-link" :href="withBase('/plugins/overview')"
            >开始开发扩展 <HomeIcon
          /></a>
          <div class="home-tech-stack">
            <span>Go</span><span>Vue 3</span><span>PostgreSQL</span
            ><span>Plugin v5</span>
          </div>
        </div>
        <div class="home-architecture">
          <div class="home-architecture-header">
            <HomeIcon name="layers" /><span>同一个平台，不同的职责</span>
          </div>
          <div class="home-architecture-types">
            <div><strong>核心模块</strong><span>身份、社区与存储</span></div>
            <div><strong>内置功能</strong><span>课表、AI 与知识</span></div>
            <div><strong>外部插件</strong><span>独立发布与安装</span></div>
            <div><strong>资源包</strong><span>主题与空间风格</span></div>
          </div>
          <div class="home-code-preview">
            <div>
              <span class="home-code-dot"></span> plugin.json
              <span>声明摘要</span>
            </div>
            <pre>{{ manifest }}</pre>
          </div>
          <a :href="withBase('/guide/module-plugin-resource-boundaries')"
            >阅读模块与插件边界 <HomeIcon name="diagonal"
          /></a>
        </div>
      </div>
    </section>

    <section
      id="start"
      class="home-guides home-container"
      aria-labelledby="guides-title"
    >
      <div class="home-guides-heading">
        <div>
          <p class="home-eyebrow">从这里开始</p>
          <h2 id="guides-title">找到你的第一步。</h2>
        </div>
        <a
          class="home-text-link"
          :href="withBase('/guide/developer-learning-path')"
          >开发者学习路线 <HomeIcon
        /></a>
      </div>
      <div class="home-guide-grid">
        <a
          v-for="guide in guides"
          :key="guide.number"
          :href="withBase(guide.href)"
          class="home-guide"
          ><div>
            <HomeIcon :name="guide.icon" /><span>{{ guide.number }}</span>
          </div>
          <h3>{{ guide.title }}</h3>
          <p>{{ guide.text }}</p>
          <HomeIcon class="home-guide-arrow" name="diagonal"
        /></a>
      </div>
    </section>

    <section
      class="home-progress home-container"
      aria-labelledby="progress-title"
    >
      <div class="home-progress-label">
        <span class="home-signal"></span> 持续开发中
      </div>
      <div class="home-progress-main">
        <h2 id="progress-title">在开放中，走向下一版。</h2>
        <p>
          当前开发主线为 <strong>{{ frontmatter.developmentStage }}</strong
          >，运行版本标识保留 <code>{{ frontmatter.runtimeVersion }}</code
          >。社区、知识与扩展能力持续完善，整版 Final 验收仍在进行。
        </p>
        <a :href="withBase('/project/current-roadmap')"
          >查看当前进展与验收范围 <HomeIcon
        /></a>
      </div>
      <span class="home-updated">更新于 {{ frontmatter.updatedAt }}</span>
    </section>

    <section class="home-join home-container" aria-labelledby="join-title">
      <div>
        <p class="home-eyebrow">OPEN SOURCE, OPEN POSSIBILITIES</p>
        <h2 id="join-title">下一段校园故事，<br />一起写。</h2>
        <p>从部署、使用到贡献代码，每一种参与都有位置。</p>
      </div>
      <div class="home-join-actions">
        <a
          class="home-button home-button-light"
          href="https://github.com/javencpdd/CampusOS"
          target="_blank"
          rel="noopener noreferrer"
          >在 GitHub 参与 <HomeIcon name="diagonal" /></a
        ><a :href="withBase('/contributing/workflow')"
          >阅读贡献指南 <HomeIcon /></a
        ><span>开源于 MIT 协议</span>
      </div>
    </section>
  </div>
</template>
