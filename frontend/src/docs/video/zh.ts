import type { DocBlock, DocExample, VideoDocsContent } from './types'

const zhBase: VideoDocsContent = {
  category: '视频生成',
  beforeProtocols: [
    {
      id: 'overview',
      title: '概览',
      blocks: [
        {
          type: 'p',
          text: '视频生成接口面向持有系统 API Key 的下游开发者。所有请求使用 Bearer 鉴权，Key 在「API 密钥」页创建：'
        },
        {
          type: 'code',
          title: '鉴权方式',
          code: 'Authorization: Bearer {{API_KEY}}'
        },
        {
          type: 'list',
          items: [
            'Base URL 为当前站点地址，本页示例已自动填入。',
            '请求入口的协议必须与 Key 所在分组中账号的协议一致，网关不做跨厂商转换；MiniMax v2 账号兼容旧版客户端的 prompt／首尾帧提交字段，v1 账号不支持 v2 请求，使用 /v2 入口须分组内有可用的 v2 账号（不会自动降级为 v1）。',
            '视频生成为异步流程：提交任务 → 轮询查询 → 下载结果。'
          ]
        },
        { type: 'link', to: '/async-tasks', text: '也可以在「异步任务」页查看进度和下载结果' }
      ]
    },
    {
      id: 'billing',
      title: '计费与余额',
      blocks: [
        {
          type: 'list',
          items: [
            '提交任务时按冻结的价格预占余额，余额检查通过后请求才会发往上游。',
            '余额 ≤ 0（或低于最低保留余额）返回 403；余额为正但不足以覆盖本次冻结金额返回 402；两种情况下请求都不会发往上游。',
            '任务成功后按实际用量结算，实扣金额不超过预占金额。',
            '任务失败或取消时，预占金额全额释放回可用余额。',
            '创建请求超时或连接中断时，任务可能已被上游接受，冻结金额会继续保留等待核实；请先在「异步任务」页或查询接口确认任务状态（或联系支持），不要把所有 502 都当作未创建而盲目重复提交。只有明确提示「Upstream request was not sent」时，才能确认本次预占已释放。',
            '生成期间的冻结金额显示在页面顶栏。',
            '重复查询不会重复扣费。'
          ]
        }
      ]
    },
    {
      id: 'download',
      title: '结果下载',
      blocks: [
        {
          type: 'p',
          text: '账号开启「隐藏上游视频链接」时，响应中的结果 URL 会被替换为本站路径，需使用创建任务的同一个系统 API Key 下载：'
        },
        {
          type: 'code',
          title: '下载生成的视频',
          code: `curl '{{BASE_URL}}/v1/video-tasks/{resource}/content?index=0' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -o generated-video.mp4`
        },
        {
          type: 'list',
          items: [
            '直接使用提交或查询响应中返回的完整结果 URL，不要用 task_id 或 operation 尾段自行拼接下载地址；多视频输出按 index 下载。',
            '其它 Key、其它用户访问该路径返回 404。',
            '开启隐藏链接时不支持 callback_url，提交前返回 400，请改用轮询查询。',
            '重复下载不会重复计费；余额耗尽后仍可读取已接受的任务。'
          ]
        }
      ]
    }
  ],
  protocolsTitle: '按协议接入',
  protocols: [
    {
      id: 'seedance',
      name: 'Seedance (Ark)',
      sections: [
        {
          id: 'seedance-endpoints',
          title: '接口',
          blocks: [
            {
              type: 'table',
              headers: ['方法', '路径', '说明'],
              rows: [
                ['POST', '/api/v3/contents/generations/tasks', '创建任务；兼容 /v3、/v1 和无前缀入口'],
                ['GET', '/api/v3/contents/generations/tasks/{task_id}', '查询任务'],
                ['DELETE', '/api/v3/contents/generations/tasks/{task_id}', '取消任务']
              ]
            },
            { type: 'p', text: '请求与响应保留 Ark 原生格式，网关只按账号配置改写模型名。每个任务固定生成 1 个视频。' }
          ]
        },
        {
          id: 'seedance-params',
          title: '主要参数',
          blocks: [
            {
              type: 'table',
              headers: ['字段', '类型', '必填', '说明'],
              rows: [
                ['model', 'string', '是', '公开模型名（分组中配置的映射名）；draft_task 正式片须与样片使用同一公开模型名'],
                ['content', 'array', '是', '内容数组：text 为文本；image_url 配合 role: first_frame／last_frame 为首尾帧；role: reference_image 为参考图；正式片为恰好一个 draft_task 对象'],
                ['duration', 'integer', '否', '时长秒数，须为整数。未填写时网关显式补默认值：1.0／1.5／2.0 为 5 秒，2.5 为 -1（智能时长）。普通整数范围：1.0 pro 2–12 秒；1.5 pro 4–12 秒；2.0 4–15 秒；2.5 4–30 秒。1.5 pro、2.0、2.5 也可显式传 -1 使用智能时长（1.0 不支持；无法确定最大时长的部署同样拒绝 -1），智能时长按最终有效最大时长预占、成功按实际时长结算'],
                ['frames', 'integer', '否', '仅 Seedance 1.0 pro／pro-fast：29–289 且满足 25+4n，按 24fps 换算，优先于 duration'],
                ['resolution', 'string', '视情况', '普通生成可选：未填写时网关显式提交 720p。draft 样片要求 480p；draft_task 正式片必须显式填写 1080p（即使等于固定档位也不能省略）'],
                ['draft', 'boolean', '否', 'true 生成样片：须 480p，已识别型号中仅 Seedance 2.5 支持样片。正式片在 content 中放恰好一个 {"type":"draft_task","draft_task":{"id":"…"}}，引用同用户、同 Key、同分组且已成功的 480p 样片，固定 1080p，不得重传 duration、ratio 等继承字段；无法识别的部署以其实际支持情况为准']
              ]
            },
            {
              type: 'p',
              text: '以上为未配置账号能力时的协议默认范围与默认值。配置账号能力后：未填写的分辨率在默认档不受支持时回退为账号支持的最低档，未填写的普通时长钳制到账号有效区间；需要固定输出参数时请显式填写。实际可用的时长和分辨率以所在分组的上游账号能力为准，超出时提交前返回 400 并提示支持范围。'
            }
          ]
        },
        {
          id: 'seedance-examples',
          title: '示例',
          blocks: [
            {
              type: 'code',
              title: '文生视频',
              code: `curl '{{BASE_URL}}/api/v3/contents/generations/tasks' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"YOUR_VIDEO_MODEL","content":[{"type":"text","text":"夕阳下的海浪"}]}'`
            },
            {
              type: 'code',
              title: '首帧图生视频',
              code: `curl '{{BASE_URL}}/api/v3/contents/generations/tasks' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"YOUR_VIDEO_MODEL","content":[{"type":"text","text":"镜头缓慢推进"},{"type":"image_url","image_url":{"url":"https://example.com/first-frame.png"},"role":"first_frame"}]}'`
            },
            {
              type: 'code',
              title: '参考图生视频',
              code: `curl '{{BASE_URL}}/api/v3/contents/generations/tasks' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"YOUR_VIDEO_MODEL","content":[{"type":"text","text":"角色在海边奔跑"},{"type":"image_url","image_url":{"url":"https://example.com/reference.png"},"role":"reference_image"}]}'`
            },
            {
              type: 'code',
              title: '生成样片（draft，480p）',
              code: `curl '{{BASE_URL}}/api/v3/contents/generations/tasks' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"YOUR_VIDEO_MODEL","content":[{"type":"text","text":"夕阳下的海浪"}],"resolution":"480p","draft":true}'`
            },
            {
              type: 'code',
              title: '用样片创建正式片（固定 1080p，不重传继承参数）',
              code: `curl '{{BASE_URL}}/api/v3/contents/generations/tasks' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"YOUR_VIDEO_MODEL","content":[{"type":"draft_task","draft_task":{"id":"DRAFT_TASK_ID"}}],"resolution":"1080p"}'`
            },
            {
              type: 'code',
              title: '查询任务',
              code: `curl '{{BASE_URL}}/api/v3/contents/generations/tasks/{task_id}' \\
  -H 'Authorization: Bearer {{API_KEY}}'`
            },
            {
              type: 'code',
              title: '取消任务',
              code: `curl -X DELETE '{{BASE_URL}}/api/v3/contents/generations/tasks/{task_id}' \\
  -H 'Authorization: Bearer {{API_KEY}}'`
            },
            {
              type: 'code',
              title: '下载结果（使用响应中返回的完整 URL）',
              code: `curl '{{BASE_URL}}/v1/video-tasks/c2VlZGFuY2U6Y2d0LWV4YW1wbGUtdGFzay1pZA/content?index=0' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -o seedance-video.mp4`
            }
          ]
        },
        {
          id: 'seedance-responses',
          title: '响应样例与状态值',
          blocks: [
            {
              type: 'code',
              title: '提交响应（Ark 原生格式）',
              code: '{"id":"cgt-example-task-id"}'
            },
            {
              type: 'code',
              title: '查询响应（成功示例，隐藏上游链接时）',
              code: '{"id":"cgt-example-task-id","model":"YOUR_VIDEO_MODEL","status":"succeeded","content":{"video_url":"/v1/video-tasks/c2VlZGFuY2U6Y2d0LWV4YW1wbGUtdGFzay1pZA/content?index=0"},"usage":{"completion_tokens":12345}}'
            },
            {
              type: 'p',
              text: '任务页将 queued／running 显示为「处理中」，succeeded 为「已完成」，failed／cancelled 为对应终态。取消成功通常返回上游 HTTP 204 空响应体，以上游实际状态为准；隐藏与非隐藏模式行为一致。下载时直接使用响应中返回的完整结果 URL，不要用任务 ID 自行拼接。'
            }
          ]
        }
      ]
    },
    {
      id: 'gemini',
      name: 'Google Gemini (Veo)',
      sections: [
        {
          id: 'gemini-endpoints',
          title: '接口',
          blocks: [
            {
              type: 'table',
              headers: ['方法', '路径', '说明'],
              rows: [
                ['POST', '/v1beta/models/{model}:predictLongRunning', '创建任务，模型以 URL 中的模型名为准'],
                ['GET', '/v1beta/{operation_name}', '查询任务；operation_name 为提交响应中的 name，形如 models/veo-3.0-generate-001/operations/xxx']
              ]
            },
            { type: 'p', text: 'instances 必须恰好包含一个对象；生成数量使用 parameters.sampleCount。本站接入的是 Gemini API（generativelanguage），不适用 Vertex AI 的 1–4 个结果上限。' }
          ]
        },
        {
          id: 'gemini-params',
          title: '主要参数',
          blocks: [
            {
              type: 'table',
              headers: ['字段', '类型', '必填', '说明'],
              rows: [
                ['instances', 'array', '是', '恰好 1 个对象；prompt 为文本；Veo 3.1 可使用 instances[0].referenceImages 参考图（最多 3 张，inlineData base64，须 8 秒）'],
                ['parameters.sampleCount', 'integer', '否', '生成数量，默认 1；veo-2.0 允许 1–2，其余型号固定 1'],
                ['parameters.durationSeconds', 'integer', '否', '默认 8 秒；veo-2.0 为 5–8 秒，veo-3.0 为 8 秒，veo-3.1 为 4／6／8 秒'],
                ['parameters.resolution', 'string', '否', '默认 720p；veo-3.0 的 1080p 仅 16:9；veo-3.1 的 1080p 或使用参考图时须 8 秒'],
                ['parameters.aspectRatio', 'string', '否', '默认显式提交 16:9']
              ]
            },
            {
              type: 'p',
              text: '以上默认值是未配置账号能力时的协议默认值，未填写的参数由网关显式写入上游请求。配置账号能力后：默认分辨率不受支持时回退为账号支持的最低档，未填写的时长钳制到账号有效区间；需要固定输出参数时请显式填写。4K 与视频扩展在提交前返回 400。实际可用的时长和分辨率以所在分组的上游账号能力为准，超出时提交前返回 400 并提示支持范围。'
            }
          ]
        },
        {
          id: 'gemini-examples',
          title: '示例',
          blocks: [
            {
              type: 'code',
              title: '文生视频',
              code: `curl '{{BASE_URL}}/v1beta/models/veo-3.0-generate-001:predictLongRunning' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"instances":[{"prompt":"夕阳下的海浪"}],"parameters":{"durationSeconds":8,"resolution":"720p"}}'`
            },
            {
              type: 'code',
              title: '首帧图生视频',
              code: `curl '{{BASE_URL}}/v1beta/models/veo-3.0-generate-001:predictLongRunning' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"instances":[{"prompt":"镜头缓慢推进","image":{"inlineData":{"mimeType":"image/png","data":"<BASE64_IMAGE>"}}}],"parameters":{"durationSeconds":8,"resolution":"720p"}}'`
            },
            {
              type: 'code',
              title: '参考图生视频（仅 Veo 3.1，最多 3 张，须 8 秒）',
              code: `curl '{{BASE_URL}}/v1beta/models/veo-3.1-generate-preview:predictLongRunning' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"instances":[{"prompt":"参考画面中的人物在海边奔跑","referenceImages":[{"image":{"inlineData":{"mimeType":"image/png","data":"<BASE64_IMAGE>"}},"referenceType":"asset"}]}],"parameters":{"durationSeconds":8,"resolution":"720p"}}'`
            },
            {
              type: 'code',
              title: '查询任务',
              code: `curl '{{BASE_URL}}/v1beta/models/veo-3.0-generate-001/operations/example-operation-id' \\
  -H 'Authorization: Bearer {{API_KEY}}'`
            },
            {
              type: 'code',
              title: '下载结果（使用响应中返回的完整 URL）',
              code: `curl '{{BASE_URL}}/v1/video-tasks/Z2VtaW5pOm1vZGVscy92ZW8tMy4wLWdlbmVyYXRlLTAwMS9vcGVyYXRpb25zL2V4YW1wbGUtb3BlcmF0aW9uLWlk/content?index=0' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -o veo-video.mp4`
            }
          ]
        },
        {
          id: 'gemini-responses',
          title: '响应样例与状态值',
          blocks: [
            {
              type: 'code',
              title: '提交响应',
              code: '{"name":"models/veo-3.0-generate-001/operations/example-operation-id"}'
            },
            {
              type: 'code',
              title: '查询响应（完成示例）',
              code: '{"name":"models/veo-3.0-generate-001/operations/example-operation-id","done":true,"response":{"generateVideoResponse":{"generatedSamples":[{"video":{"uri":"/v1/video-tasks/Z2VtaW5pOm1vZGVscy92ZW8tMy4wLWdlbmVyYXRlLTAwMS9vcGVyYXRpb25zL2V4YW1wbGUtb3BlcmF0aW9uLWlk/content?index=0"}}]}}}'
            },
            {
              type: 'p',
              text: 'done 为 false 表示处理中；done 为 true 表示操作已结束，但还需响应中包含有效的生成视频结果才算成功：error 字段（Google 格式 code／message／status）或空输出（无可用视频结果）都会使任务失败，不能仅凭 done 且无 error 判断可下载。下载时直接使用响应中返回的完整结果 URL，不要用 operation 尾段自行拼接。'
            }
          ]
        }
      ]
    },
    {
      id: 'minimax-v2',
      name: 'MiniMax H3 (v2)',
      sections: [
        {
          id: 'minimax-v2-endpoints',
          title: '接口',
          blocks: [
            {
              type: 'table',
              headers: ['方法', '路径', '说明'],
              rows: [
                ['POST', '/v2/video_generation', '创建任务；也接受旧版 prompt／首尾帧字段并转为 content'],
                ['GET', '/v2/query/video_generation/{task_id}', '查询任务，使用路径中的任务 ID']
              ]
            },
            { type: 'p', text: '每个请求须包含非空文本；首尾帧（first_frame／last_frame）不可与参考媒体混用。GET /v2/query/video_generation?task_id=... 是上游任务列表，网关不开放该列表。' }
          ]
        },
        {
          id: 'minimax-v2-params',
          title: '主要参数',
          blocks: [
            {
              type: 'table',
              headers: ['字段', '类型', '必填', '说明'],
              rows: [
                ['model', 'string', '是', 'MiniMax-H3／MiniMax-H3-Max 或分组配置的映射名'],
                ['content', 'array', '原生 v2 必填', '内容数组，须含非空 text；image_url 配合 role: first_frame／last_frame 为首尾帧；role: reference_image 为参考图（最多 9 张，不可与首尾帧混用）。使用兼容的 prompt／首尾帧字段提交时可省略，由网关转换'],
                ['duration', 'integer', '否', '默认 6 秒；官方范围：H3 为 4–15 秒，H3-Max 为 5–15 秒'],
                ['resolution', 'string', '否', '默认 768P；H3 支持 768P／2K，H3-Max 支持 480P／768P（不支持 2K）'],
                ['ratio', 'string', '否', '文生视频必填，如 16:9；图生可使用 adaptive']
              ]
            },
            {
              type: 'p',
              text: '以上默认值是未配置账号能力时的协议默认值。配置账号能力后：默认分辨率不受支持时回退为账号支持的最低档，未填写的时长钳制到账号有效区间；需要固定输出参数时请显式填写。实际可用的时长和分辨率以所在分组的上游账号能力为准（例如秘塔 MiniMax-H3 可配置更大范围），超出时提交前返回 400 并提示支持范围。'
            }
          ]
        },
        {
          id: 'minimax-v2-examples',
          title: '示例',
          blocks: [
            {
              type: 'code',
              title: '文生视频',
              code: `curl '{{BASE_URL}}/v2/video_generation' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"MiniMax-H3","content":[{"type":"text","text":"夕阳下的海浪"}],"duration":6,"resolution":"768P","ratio":"16:9"}'`
            },
            {
              type: 'code',
              title: '首帧图生视频',
              code: `curl '{{BASE_URL}}/v2/video_generation' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"MiniMax-H3","content":[{"type":"text","text":"镜头缓慢推进"},{"type":"image_url","image_url":{"url":"https://example.com/first-frame.png"},"role":"first_frame"}],"duration":6,"resolution":"768P","ratio":"adaptive"}'`
            },
            {
              type: 'code',
              title: '参考图生视频（最多 9 张，不可与首尾帧混用）',
              code: `curl '{{BASE_URL}}/v2/video_generation' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"MiniMax-H3","content":[{"type":"text","text":"参考画面中的人物在海边奔跑"},{"type":"image_url","image_url":{"url":"https://example.com/reference.png"},"role":"reference_image"}],"duration":6,"resolution":"768P"}'`
            },
            {
              type: 'code',
              title: '查询任务',
              code: `curl '{{BASE_URL}}/v2/query/video_generation/{task_id}' \\
  -H 'Authorization: Bearer {{API_KEY}}'`
            },
            {
              type: 'code',
              title: '下载结果（使用响应中返回的完整 URL）',
              code: `curl '{{BASE_URL}}/v1/video-tasks/bWluaW1heDpleGFtcGxlLXRhc2staWQ/content?index=0' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -o minimax-video.mp4`
            }
          ]
        },
        {
          id: 'minimax-v2-responses',
          title: '响应样例与状态值',
          blocks: [
            {
              type: 'code',
              title: '提交响应',
              code: '{"task_id":"example-task-id"}'
            },
            {
              type: 'code',
              title: '查询响应（成功示例，隐藏上游链接时）',
              code: '{"task":{"id":"example-task-id","status":"succeeded","content":{"url":"/v1/video-tasks/bWluaW1heDpleGFtcGxlLXRhc2staWQ/content?index=0"}}}'
            },
            {
              type: 'p',
              text: '状态值：queued／running 为处理中，succeeded 为已完成，failed／cancelled 为对应终态。成功结果来自 task.content.url；下载时直接使用响应中返回的完整结果 URL，不要用任务 ID 自行拼接。'
            }
          ]
        }
      ]
    },
    {
      id: 'minimax-v1',
      name: 'MiniMax Hailuo (v1)',
      sections: [
        {
          id: 'minimax-v1-endpoints',
          title: '接口',
          blocks: [
            {
              type: 'table',
              headers: ['方法', '路径', '说明'],
              rows: [
                ['POST', '/v1/video_generation', '创建任务，文生、图生共用入口'],
                ['GET', '/v1/query/video_generation?task_id={task_id}', '查询任务状态'],
                ['GET', '/v1/files/retrieve?file_id={file_id}', '获取文件下载信息，file_id 来自成功任务的响应']
              ]
            },
            { type: 'p', text: 'first_frame_image、last_frame_image、subject_reference、prompt_optimizer 等原生字段透传，仅按账号映射改写模型。' }
          ]
        },
        {
          id: 'minimax-v1-params',
          title: '主要参数',
          blocks: [
            {
              type: 'table',
              headers: ['字段', '类型', '必填', '说明'],
              rows: [
                ['model', 'string', '是', '公开模型名（分组中配置的映射名）'],
                ['prompt', 'string', '文生必填', '文本提示词；图生至少提供文本或输入图之一（first_frame_image 等），具体型号限制由上游校验'],
                ['duration', 'integer', '否', '默认 6 秒；沿用 6／10 秒规则，具体型号限制由上游校验'],
                ['resolution', 'string', '否', '未填写时网关显式提交 768P；512P 使用 480P 计费档，旧模型请显式指定其支持的分辨率']
              ]
            },
            {
              type: 'p',
              text: '以上默认值是未配置账号能力时的协议默认值。配置账号能力后：默认分辨率不受支持时回退为账号支持的最低档，未填写的时长钳制到账号有效区间；需要固定输出参数时请显式填写。实际可用的时长和分辨率以所在分组的上游账号能力为准，超出时提交前返回 400 并提示支持范围。'
            }
          ]
        },
        {
          id: 'minimax-v1-examples',
          title: '示例',
          blocks: [
            {
              type: 'code',
              title: '文生视频',
              code: `curl '{{BASE_URL}}/v1/video_generation' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"YOUR_VIDEO_MODEL","prompt":"夕阳下的海浪","duration":6,"resolution":"1080P"}'`
            },
            {
              type: 'code',
              title: '首帧图生视频',
              code: `curl '{{BASE_URL}}/v1/video_generation' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"YOUR_VIDEO_MODEL","prompt":"镜头缓慢推进","first_frame_image":"https://example.com/first-frame.png","duration":6,"resolution":"768P"}'`
            },
            {
              type: 'code',
              title: '查询任务',
              code: `curl '{{BASE_URL}}/v1/query/video_generation?task_id={task_id}' \\
  -H 'Authorization: Bearer {{API_KEY}}'`
            },
            {
              type: 'code',
              title: '获取文件下载信息',
              code: `curl '{{BASE_URL}}/v1/files/retrieve?file_id={file_id}' \\
  -H 'Authorization: Bearer {{API_KEY}}'`
            },
            {
              type: 'code',
              title: '下载结果（使用响应中返回的完整 URL）',
              code: `curl '{{BASE_URL}}/v1/video-tasks/bWluaW1heDpleGFtcGxlLXRhc2staWQ/content?index=0' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -o hailuo-video.mp4`
            }
          ]
        },
        {
          id: 'minimax-v1-responses',
          title: '响应样例与状态值',
          blocks: [
            {
              type: 'code',
              title: '提交响应',
              code: '{"task_id":"example-task-id","base_resp":{"status_code":0,"status_msg":"success"}}'
            },
            {
              type: 'p',
              text: '状态值：Preparing／Queueing／Processing 为处理中，Success 为已完成，Fail 为失败。提交创建时 HTTP 200 中非零 base_resp.status_code 按创建失败处理，不收取生成费用。查询确认 Success 后，服务端用相同账号获取文件链接；此后文件信息获取失败不会撤销已结算的生成费用，重试查询或下载不重复收费。'
            }
          ]
        }
      ]
    }
  ],
  afterProtocols: [
    {
      id: 'validation',
      title: '请求校验规则',
      blocks: [
        {
          type: 'list',
          items: [
            'JSON 请求体不得包含重复键（包括转义后同名的键）或多个 JSON 值，嵌套不超过 128 层。',
            '不支持的跨协议数量或用量字段（例如在 Seedance 中传 n、sampleCount）在上游调用前返回 400；Seedance、MiniMax 每个任务固定生成 1 个视频。',
            '未填写的时长和分辨率由网关显式补默认值（各协议的默认值见上方参数表），保证上游实际生成与价格合同一致。',
            '实际费用始终不超过预占金额；若上游返回的用量超出合同，按预占金额结算。'
          ]
        }
      ]
    },
    {
      id: 'errors',
      title: '错误码',
      blocks: [
        {
          type: 'table',
          headers: ['状态码', '含义'],
          rows: [
            ['400', '参数非法；未配置视频价格（video_pricing_not_configured，Seedance／MiniMax 本地错误的 type 字段）；超出上游账号能力范围（提示支持范围）'],
            ['401', '未认证或 API Key 无效'],
            ['402', '余额不足以覆盖本次冻结金额，请求未发往上游；也可能是上游 402 透传'],
            ['403', '余额 ≤ 0（或低于最低保留余额），或无权限访问'],
            ['404', '任务不存在，或不属于当前 Key／用户'],
            ['413', '请求体超过大小限制'],
            ['429', '超出配额或限额'],
            ['502', '上游错误；仅「Upstream request was not sent」表示请求未发出且预占已释放，其它超时、断连等传输故障可能已被上游接受，预占会保留等待核实，请先确认任务状态再决定是否重新提交'],
            ['503', '分组内没有该协议的可用账号（可能未配置、已禁用或繁忙），例如 No eligible Seedance accounts；错误信息会提示其它协议对应的请求入口，请确认使用的入口与分组账号协议一致']
          ]
        },
        {
          type: 'p',
          text: '网关本地产生的错误：Gemini 入口使用 Google 格式（error.code／message／status），Seedance、MiniMax 入口使用 error.type／message 格式。上游原生错误按各厂商原生格式透传，不会被统一归一化：Seedance 返回 Ark 原生结构（如 error.code／message）；MiniMax 保留原生 JSON 结构（上游 v1 可能以 HTTP 200 加非零 base_resp.status_code／status_msg 表示失败；网关保留原生 JSON 错误体，但可能把这类业务失败映射为 502 或 429，请同时检查 HTTP 状态与业务字段），仅非 JSON 的 4xx／5xx 会包装为通用 error.type／message；Gemini 的原生上游错误同样保留原生结构。'
        }
      ]
    }
  ]
}

type ExampleSpec = Omit<DocExample, 'type' | 'request'> & { requestTitle: string }

const responseHeaders = 'Content-Type: video/mp4\nContent-Disposition: attachment; filename="generated-video.mp4"\nCache-Control: no-store'

function responseTable(rows: string[][], appendRoot = true): DocBlock {
  const hasRoot = rows.some(row => row[0] === '{root}')
  const isDownload = rows.some(row => row[0] === 'response body')
  const completeRows = hasRoot || isDownload || !appendRoot
    ? rows
    : [...rows, ['{root}', 'object', '其它字段按上游结构透传；开启隐藏链接时，所有上游 URL 字符串及含 URL 的响应头会按隐私规则脱敏或删除，以厂商文档为准。']]
  return { type: 'table', headers: ['字段路径', '类型', '说明'], rows: completeRows }
}

const responseTables: Record<string, string[][][]> = {
  seedance: [
    [['id', 'string', '提交成功返回的 Seedance 任务 ID（如 cgt-…）。']],
    [
      ['id', 'string', '任务 ID。'],
      ['model', 'string', '上游返回的模型名，按 Ark 原生响应透传。'],
      ['status', 'string', 'queued／running 表示处理中；succeeded 表示成功；failed／cancelled／expired 表示终态。'],
      ['content.video_url', 'string', '成功视频 URL；开启隐藏上游链接时替换为本站 /v1/video-tasks/{resource}/content?index=N。'],
      ['usage.completion_tokens', 'integer', '成功响应中可能返回的用量字段，按上游原样透传。']
    ],
    [
      ['HTTP status', 'integer', '取消成功通常返回 204 No Content；以上游实际状态为准，隐藏与非隐藏模式行为一致。'],
      ['response body', 'empty', '取消成功响应体为空，不包含 JSON 根对象。']
    ],
    [
      ['HTTP status', 'integer', '成功返回 200；非法 resource 或 index 返回 400；未认证返回 401；任务不存在或无权访问返回 404；上游非 200 或 Content-Type 不是视频时返回错误或 502。'],
      ['response body', 'binary', '成功时返回视频二进制，不包含 JSON 响应体；错误时返回网关或任务错误对象。'],
      ['Content-Type', 'string', '上游返回的 video/* 或 application/octet-stream。'],
      ['Content-Disposition', 'string', '网关设置为 attachment; filename="generated-video.mp4"。'],
      ['Cache-Control', 'string', '网关设置为 no-store。']
    ]
  ],
  gemini: [
    [['name', 'string', '提交成功返回的 operation name，形如 models/{model}/operations/{id}。']],
    [
      ['name', 'string', '操作名称；后续查询使用完整 name。'],
      ['done', 'boolean', 'false 表示处理中；true 表示操作已结束。'],
      ['response.generateVideoResponse.generatedSamples[].video.uri', 'string', '成功视频 URL；开启隐藏上游链接时替换为本站 /v1/video-tasks/{resource}/content?index=N。'],
      ['error.code / message / status', 'number / string', 'Google 原生错误结构；出现 error 或空视频结果时任务失败。']
    ],
    [
      ['HTTP status', 'integer', '成功返回 200；非法 resource 或 index 返回 400；未认证返回 401；任务不存在或无权访问返回 404；上游非 200 或 Content-Type 不是视频时返回错误或 502。'],
      ['response body', 'binary', '成功时返回视频二进制，不包含 JSON 响应体；错误时返回网关或任务错误对象。'],
      ['Content-Type', 'string', '上游返回的 video/* 或 application/octet-stream。'],
      ['Content-Disposition', 'string', '网关设置为 attachment; filename="generated-video.mp4"。'],
      ['Cache-Control', 'string', '网关设置为 no-store。']
    ]
  ],
  'minimax-v2': [
    [['task_id', 'string', '提交成功返回的 MiniMax v2 任务 ID；v2 成功提交的根对象只包含 task_id，错误响应按网关或上游错误契约返回。']],
    [
      ['task.id', 'string', '任务 ID。'],
      ['task.status', 'string', 'queued／running 表示处理中；succeeded 表示成功；failed／cancelled 表示终态。'],
      ['task.content.url', 'string', '成功视频 URL；开启隐藏上游链接时替换为本站 /v1/video-tasks/{resource}/content?index=N。'],
      ['task.error.message', 'string', '若上游响应包含此字段，网关会读取并透传；该字段可能不存在。']
    ],
    [
      ['HTTP status', 'integer', '成功返回 200；非法 resource 或 index 返回 400；未认证返回 401；任务不存在或无权访问返回 404；上游非 200 或 Content-Type 不是视频时返回错误或 502。'],
      ['response body', 'binary', '成功时返回视频二进制，不包含 JSON 响应体；错误时返回网关或任务错误对象。'],
      ['Content-Type', 'string', '上游返回的 video/* 或 application/octet-stream。'],
      ['Content-Disposition', 'string', '网关设置为 attachment; filename="generated-video.mp4"。'],
      ['Cache-Control', 'string', '网关设置为 no-store。']
    ]
  ],
  'minimax-v1': [
    [
      ['task_id', 'string', '提交成功返回的 MiniMax v1 任务 ID。'],
      ['base_resp.status_code', 'integer', '0 表示提交成功；HTTP 200 中非零值按创建失败处理且不收取生成费用。'],
      ['base_resp.status_msg', 'string', '上游业务状态说明。']
    ],
    [
      ['task_id', 'string', '被查询的任务 ID。'],
      ['status', 'string', 'Preparing／Queueing／Processing 表示处理中；Success 表示成功；Fail 表示失败。'],
      ['file_id', 'string', 'Success 时的文件 ID，用于调用 files/retrieve。'],
      ['base_resp.status_code / status_msg', 'integer / string', 'MiniMax 原生业务状态字段，按上游原样透传。']
    ],
    [
      ['file.file_id', 'string', '文件 ID，应与请求中的 file_id 一致。'],
      ['file.download_url', 'string', '短期有效的下载 URL；开启隐藏上游链接时改写为本站 /v1/video-tasks/{resource}/content?index=N。'],
      ['base_resp.status_code / status_msg', 'integer / string', 'MiniMax 原生业务状态字段。']
    ],
    [
      ['HTTP status', 'integer', '成功返回 200；非法 resource 或 index 返回 400；未认证返回 401；任务不存在或无权访问返回 404；上游非 200 或 Content-Type 不是视频时返回错误或 502。'],
      ['response body', 'binary', '成功时返回视频二进制，不包含 JSON 响应体；错误时返回网关或任务错误对象。'],
      ['Content-Type', 'string', '上游返回的 video/* 或 application/octet-stream。'],
      ['Content-Disposition', 'string', '网关设置为 attachment; filename="generated-video.mp4"。'],
      ['Cache-Control', 'string', '网关设置为 no-store。']
    ]
  ]
}

const responseTableLabels: Record<string, string[]> = {
  seedance: ['POST /api/v3/contents/generations/tasks', 'GET /api/v3/contents/generations/tasks/{task_id}', 'DELETE /api/v3/contents/generations/tasks/{task_id}', 'GET /v1/video-tasks/{resource}/content'],
  gemini: ['POST /v1beta/models/{model}:predictLongRunning', 'GET /v1beta/{operation_name}', 'GET /v1/video-tasks/{resource}/content'],
  'minimax-v2': ['POST /v2/video_generation', 'GET /v2/query/video_generation/{task_id}', 'GET /v1/video-tasks/{resource}/content'],
  'minimax-v1': ['POST /v1/video_generation', 'GET /v1/query/video_generation?task_id={task_id}', 'GET /v1/files/retrieve?file_id={file_id}', 'GET /v1/video-tasks/{resource}/content']
}

const exampleSpecs: Record<string, ExampleSpec[]> = {
  seedance: [
    { id: 'text-to-video', title: '文生视频', description: '提交文本提示词并创建异步任务。', requestTitle: '文生视频', responses: [{ title: '已接受', status: 200, body: '{"id":"cgt-example-task-id"}', description: 'Seedance 返回任务 ID。' }] },
    { id: 'first-frame', title: '首帧图生视频', description: '使用一张首帧图和文本提示词。', requestTitle: '首帧图生视频', responses: [{ title: '已接受', status: 200, body: '{"id":"cgt-example-task-id"}' }] },
    { id: 'reference-image', title: '参考图生视频', description: '使用参考图引导视频内容。', requestTitle: '参考图生视频', responses: [{ title: '已接受', status: 200, body: '{"id":"cgt-example-task-id"}' }] },
    { id: 'draft', title: '生成样片（draft，480p）', description: 'Seedance 2.5 样片请求固定使用 480p。', requestTitle: '生成样片（draft，480p）', responses: [{ title: '已接受', status: 200, body: '{"id":"cgt-draft-task-id"}' }] },
    { id: 'draft-task', title: '用样片创建正式片（固定 1080p）', description: '引用已成功样片，不重传继承的时长和比例字段。', requestTitle: '用样片创建正式片（固定 1080p，不重传继承参数）', responses: [{ title: '已接受', status: 200, body: '{"id":"cgt-final-task-id"}' }] },
    { id: 'query', title: '查询任务', description: '轮询同一个 task_id，直到任务进入终态。', requestTitle: '查询任务', responses: [
      { title: '处理中', status: 200, body: '{"id":"cgt-example-task-id","status":"running"}' },
      { title: '已成功', status: 200, body: '{"id":"cgt-example-task-id","model":"YOUR_VIDEO_MODEL","status":"succeeded","content":{"video_url":"/v1/video-tasks/c2VlZGFuY2U6Y2d0LWV4YW1wbGUtdGFzay1pZA/content?index=0"},"usage":{"completion_tokens":12345}}' }
    ] },
    { id: 'cancel', title: '取消任务', description: '取消仍在处理中的 Seedance 任务。', requestTitle: '取消任务', responses: [{ title: '已取消', status: 204, description: '取消成功通常为空的 HTTP 204 响应体，以上游实际状态为准；隐藏与非隐藏模式行为一致。' }] },
    { id: 'download', title: '下载结果', description: '使用响应中返回的完整 opaque resource URL 下载视频。', requestTitle: '下载结果（使用响应中返回的完整 URL）', responses: [{ title: '视频内容', status: 200, headers: responseHeaders, description: '响应体为视频二进制，不在文档中放置二进制内容。' }] }
  ],
  gemini: [
    { id: 'text-to-video', title: '文生视频', description: '调用 predictLongRunning 创建操作。', requestTitle: '文生视频', responses: [{ title: '已接受', status: 200, body: '{"name":"models/veo-3.0-generate-001/operations/example-operation-id"}' }] },
    { id: 'first-frame', title: '首帧图生视频', description: '使用 inlineData 提供首帧图。', requestTitle: '首帧图生视频', responses: [{ title: '已接受', status: 200, body: '{"name":"models/veo-3.0-generate-001/operations/example-operation-id"}' }] },
    { id: 'reference-image', title: '参考图生视频', description: 'Veo 3.1 使用 referenceImages，最多三张且须 8 秒。', requestTitle: '参考图生视频（仅 Veo 3.1，最多 3 张，须 8 秒）', responses: [{ title: '已接受', status: 200, body: '{"name":"models/veo-3.1-generate-preview/operations/example-operation-id"}' }] },
    { id: 'query', title: '查询任务', description: '必须复制提交响应中的完整 name，并使用该完整 operation name 查询任务。', requestTitle: '查询任务', responses: [
      { title: '处理中', status: 200, body: '{"name":"models/veo-3.0-generate-001/operations/example-operation-id","done":false}' },
      { title: '已成功', status: 200, body: '{"name":"models/veo-3.0-generate-001/operations/example-operation-id","done":true,"response":{"generateVideoResponse":{"generatedSamples":[{"video":{"uri":"/v1/video-tasks/Z2VtaW5pOm1vZGVscy92ZW8tMy4wLWdlbmVyYXRlLTAwMS9vcGVyYXRpb25zL2V4YW1wbGUtb3BlcmF0aW9uLWlk/content?index=0"}}]}}}' }
    ] },
    { id: 'download', title: '下载结果', description: '直接使用查询响应中的完整结果 URL。', requestTitle: '下载结果（使用响应中返回的完整 URL）', responses: [{ title: '视频内容', status: 200, headers: responseHeaders, description: '响应体为视频二进制，不在文档中放置二进制内容。' }] }
  ],
  'minimax-v2': [
    { id: 'text-to-video', title: '文生视频', description: '使用 H3 content 文本创建任务。', requestTitle: '文生视频', responses: [{ title: '已接受', status: 200, body: '{"task_id":"example-task-id"}' }] },
    { id: 'first-frame', title: '首帧图生视频', description: '使用 first_frame role 提供首帧。', requestTitle: '首帧图生视频', responses: [{ title: '已接受', status: 200, body: '{"task_id":"example-task-id"}' }] },
    { id: 'reference-image', title: '参考图生视频', description: '使用 reference_image role，不能与首尾帧混用。', requestTitle: '参考图生视频（最多 9 张，不可与首尾帧混用）', responses: [{ title: '已接受', status: 200, body: '{"task_id":"example-task-id"}' }] },
    { id: 'query', title: '查询任务', description: '使用路径中的 task_id 查询任务。', requestTitle: '查询任务', responses: [
      { title: '处理中', status: 200, body: '{"task":{"id":"example-task-id","status":"running"}}' },
      { title: '已成功', status: 200, body: '{"task":{"id":"example-task-id","status":"succeeded","content":{"url":"/v1/video-tasks/bWluaW1heDpleGFtcGxlLXRhc2staWQ/content?index=0"}}}' }
    ] },
    { id: 'download', title: '下载结果', description: '使用查询响应中返回的完整结果 URL。', requestTitle: '下载结果（使用响应中返回的完整 URL）', responses: [{ title: '视频内容', status: 200, headers: responseHeaders, description: '响应体为视频二进制，不在文档中放置二进制内容。' }] }
  ],
  'minimax-v1': [
    { id: 'text-to-video', title: '文生视频', description: '使用 prompt 创建 Hailuo v1 任务。', requestTitle: '文生视频', responses: [{ title: '已接受', status: 200, body: '{"task_id":"example-task-id","base_resp":{"status_code":0,"status_msg":"success"}}' }] },
    { id: 'first-frame', title: '首帧图生视频', description: '使用 first_frame_image 创建图生任务。', requestTitle: '首帧图生视频', responses: [{ title: '已接受', status: 200, body: '{"task_id":"example-task-id","base_resp":{"status_code":0,"status_msg":"success"}}' }] },
    { id: 'query', title: '查询任务', description: '轮询任务状态；Success 响应会给出 file_id。', requestTitle: '查询任务', responses: [
      { title: '处理中', status: 200, body: '{"task_id":"example-task-id","status":"Processing","base_resp":{"status_code":0,"status_msg":"success"}}' },
      { title: '已成功', status: 200, body: '{"task_id":"example-task-id","status":"Success","file_id":"example-file-id","base_resp":{"status_code":0,"status_msg":"success"}}' }
    ] },
    { id: 'file', title: '获取文件下载信息', description: '使用成功任务返回的 file_id 获取短期下载 URL。', requestTitle: '获取文件下载信息', responses: [{ title: '成功', status: 200, body: '{"file":{"file_id":"example-file-id","download_url":"/v1/video-tasks/bWluaW1heC1maWxlOmV4YW1wbGUtZmlsZS1pZA/content?index=0"},"base_resp":{"status_code":0,"status_msg":"success"}}' }] },
    { id: 'download', title: '下载结果', description: '使用响应中返回的完整结果 URL 下载视频。', requestTitle: '下载结果（使用响应中返回的完整 URL）', responses: [{ title: '视频内容', status: 200, headers: responseHeaders, description: '响应体为视频二进制，不在文档中放置二进制内容。' }] }
  ]
}

function enrichDocs(base: VideoDocsContent): VideoDocsContent {
  const formatJson = (value: string): string => {
    try {
      return JSON.stringify(JSON.parse(value), null, 2)
    } catch {
      return value
    }
  }
  const formatBlock = (block: DocBlock): DocBlock => {
    if (block.type === 'code') return { ...block, code: formatJson(block.code) }
    if (block.type === 'example') {
      return {
        ...block,
        responses: block.responses.map(response => response.body ? { ...response, body: formatJson(response.body) } : response)
      }
    }
    return block
  }
  const protocols = base.protocols.map(protocol => {
    const tables = responseTables[protocol.id] ?? []
    const endpointIndex = protocol.sections.findIndex(section => section.id.endsWith('-endpoints'))
    const endpoint = protocol.sections[endpointIndex]
    const labels = responseTableLabels[protocol.id] ?? []
    const endpointBlocks = endpoint
      ? [...endpoint.blocks, ...tables.flatMap((rows, index) => [{ type: 'p' as const, text: `响应参数（${labels[index] ?? `接口 ${index + 1}`}）` }, responseTable(rows, !(protocol.id === 'minimax-v2' && index === 0))])]
      : []
    const specs = exampleSpecs[protocol.id] ?? []
    const exampleSectionIndex = protocol.sections.findIndex(section => section.id.endsWith('-examples'))
    const exampleSection = protocol.sections[exampleSectionIndex]
    const codeByTitle = new Map((exampleSection?.blocks ?? []).filter((block): block is Extract<DocBlock, { type: 'code' }> => block.type === 'code').map(block => [block.title, block.code]))
    const examples: DocExample[] = specs.map(spec => ({ type: 'example', id: spec.id, title: spec.title, description: spec.description, request: codeByTitle.get(spec.requestTitle) ?? '', responses: spec.responses }))
    const sections = protocol.sections.map((section, index) => {
      if (index === endpointIndex) return { ...section, blocks: endpointBlocks }
      if (index === exampleSectionIndex) return { ...section, blocks: examples }
      return section
    }).map(section => ({ ...section, blocks: section.blocks.map(formatBlock) }))
    return { ...protocol, sections }
  })
  return { ...base, protocols }
}

const zh = enrichDocs(zhBase)

export default zh
