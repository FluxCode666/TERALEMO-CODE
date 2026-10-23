import type { VideoDocsContent } from './types'

const en: VideoDocsContent = {
  category: 'Video Generation',
  beforeProtocols: [
    {
      id: 'overview',
      title: 'Overview',
      blocks: [
        {
          type: 'p',
          text: 'The video generation API is for downstream developers holding a system API key. All requests use Bearer authentication; keys are created on the "API Keys" page:'
        },
        {
          type: 'code',
          title: 'Authentication',
          code: 'Authorization: Bearer {{API_KEY}}'
        },
        {
          type: 'list',
          items: [
            'The Base URL is the origin of this site; the examples on this page fill it in automatically.',
            'The protocol of your request entry point must match the protocol of the accounts in the group your key belongs to — the gateway does not convert between vendors. MiniMax v2 accounts accept legacy prompt / first/last-frame submit fields; v1 accounts do not support v2 requests, and the /v2 entry requires an available v2 account in the group (it does not fall back to v1 automatically).',
            'Video generation is asynchronous: submit a task → poll for the result → download the video.'
          ]
        },
        { type: 'link', to: '/async-tasks', text: 'You can also track progress and download results on the "Async tasks" page' }
      ]
    },
    {
      id: 'billing',
      title: 'Billing & Balance',
      blocks: [
        {
          type: 'list',
          items: [
            'Submitting a task freezes a hold for the quoted price; the request is only sent upstream after the balance check passes.',
            'A balance ≤ 0 (or below the minimum reserved balance) returns 403; a positive balance that cannot cover the hold returns 402. In both cases the request is not sent upstream.',
            'On success the task settles at actual usage, never more than the hold.',
            'On failure or cancellation the hold is fully released back to the available balance.',
            'If the create request times out or the connection drops, the task may already have been accepted upstream and the hold is kept pending verification. Check the task status on the "Async tasks" page or via the query endpoint first (or contact support) — do not treat every 502 as "not created" and resubmit blindly. Only an explicit "Upstream request was not sent" message confirms the hold was released.',
            'Frozen amounts during generation are shown in the top bar.',
            'Repeated queries do not charge again.'
          ]
        }
      ]
    },
    {
      id: 'download',
      title: 'Downloading Results',
      blocks: [
        {
          type: 'p',
          text: 'When "hide upstream video URL" is enabled on the account, result URLs in responses are replaced with paths on this site and must be downloaded with the same system API key that created the task:'
        },
        {
          type: 'code',
          title: 'Download the generated video',
          code: `curl '{{BASE_URL}}/v1/video-tasks/{resource}/content?index=0' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -o generated-video.mp4`
        },
        {
          type: 'list',
          items: [
            'Use the exact result URL returned by the submit or query response — do not build the download address from the task_id or the trailing segment of an operation name. Multi-video outputs are downloaded by index.',
            'Other keys and other users get 404 on this path.',
            'callback_url is not supported when URL hiding is enabled; the request is rejected with 400 before submission. Use polling instead.',
            'Repeated downloads do not charge again; accepted tasks remain readable even after the balance is exhausted.'
          ]
        }
      ]
    }
  ],
  protocolsTitle: 'Per-Protocol Integration',
  protocols: [
    {
      id: 'seedance',
      name: 'Seedance (Ark)',
      sections: [
        {
          id: 'seedance-endpoints',
          title: 'Endpoints',
          blocks: [
            {
              type: 'table',
              headers: ['Method', 'Path', 'Description'],
              rows: [
                ['POST', '/api/v3/contents/generations/tasks', 'Create a task; /v3, /v1, and no-prefix compatibility entries are also accepted'],
                ['GET', '/api/v3/contents/generations/tasks/{task_id}', 'Query a task'],
                ['DELETE', '/api/v3/contents/generations/tasks/{task_id}', 'Cancel a task']
              ]
            },
            { type: 'p', text: 'Requests and responses keep the Ark native format; the gateway only rewrites the model name per account configuration. Each task generates exactly 1 video.' }
          ]
        },
        {
          id: 'seedance-params',
          title: 'Main parameters',
          blocks: [
            {
              type: 'table',
              headers: ['Field', 'Type', 'Required', 'Description'],
              rows: [
                ['model', 'string', 'Yes', 'Public model name (the mapped name configured in the group); a draft_task official film must use the same public model name as its draft'],
                ['content', 'array', 'Yes', 'Content array: text parts are prompts; image_url with role: first_frame / last_frame are first/last frames; role: reference_image is a reference image; an official film is exactly one draft_task object'],
                ['duration', 'integer', 'No', 'Duration in seconds, must be an integer. When omitted the gateway fills an explicit default: 5s for 1.0 / 1.5 / 2.0, -1 (smart duration) for 2.5. Regular integer ranges: 1.0 pro 2–12s; 1.5 pro 4–12s; 2.0 4–15s; 2.5 4–30s. 1.5 pro, 2.0, and 2.5 also accept an explicit -1 for smart duration (1.0 does not; deployments whose maximum duration is unknown also reject -1). Smart duration holds at the effective maximum and settles at the actual duration on success'],
                ['frames', 'integer', 'No', 'Seedance 1.0 pro / pro-fast only: 29–289 and 25+4n, converted at 24fps; takes precedence over duration'],
                ['resolution', 'string', 'Depends', 'Optional for regular generation: when omitted the gateway submits 720p explicitly. Drafts require 480p; a draft_task official film must specify 1080p explicitly (it cannot be omitted even though the tier is fixed)'],
                ['draft', 'boolean', 'No', 'true generates a draft (sample): must be 480p, and among recognized models only Seedance 2.5 supports drafts. An official film carries exactly one {"type":"draft_task","draft_task":{"id":"…"}} in content, referencing a succeeded 480p draft from the same user, key, and group; it is fixed at 1080p and inherited fields such as duration and ratio must not be re-sent; unrecognized deployments follow their actual support']
              ]
            },
            {
              type: 'p',
              text: 'The ranges and defaults above are the protocol defaults when no account capability is configured. With a capability configured: an omitted resolution whose default tier is unsupported falls back to the lowest tier the account supports, and an omitted regular duration is clamped to the account\'s effective range; set parameters explicitly when you need fixed output. Actual duration/resolution availability follows the upstream account capabilities of your group — out-of-range values are rejected with 400 before submission, with the supported range in the message.'
            }
          ]
        },
        {
          id: 'seedance-examples',
          title: 'Examples',
          blocks: [
            {
              type: 'code',
              title: 'Text to video',
              code: `curl '{{BASE_URL}}/api/v3/contents/generations/tasks' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"YOUR_VIDEO_MODEL","content":[{"type":"text","text":"Waves under the setting sun"}]}'`
            },
            {
              type: 'code',
              title: 'Image to video (first frame)',
              code: `curl '{{BASE_URL}}/api/v3/contents/generations/tasks' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"YOUR_VIDEO_MODEL","content":[{"type":"text","text":"The camera pushes in slowly"},{"type":"image_url","image_url":{"url":"https://example.com/first-frame.png"},"role":"first_frame"}]}'`
            },
            {
              type: 'code',
              title: 'Reference image to video',
              code: `curl '{{BASE_URL}}/api/v3/contents/generations/tasks' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"YOUR_VIDEO_MODEL","content":[{"type":"text","text":"The character runs along the beach"},{"type":"image_url","image_url":{"url":"https://example.com/reference.png"},"role":"reference_image"}]}'`
            },
            {
              type: 'code',
              title: 'Create a draft (480p sample)',
              code: `curl '{{BASE_URL}}/api/v3/contents/generations/tasks' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"YOUR_VIDEO_MODEL","content":[{"type":"text","text":"Waves under the setting sun"}],"resolution":"480p","draft":true}'`
            },
            {
              type: 'code',
              title: 'Create the official film from a draft (fixed 1080p, no inherited fields)',
              code: `curl '{{BASE_URL}}/api/v3/contents/generations/tasks' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"YOUR_VIDEO_MODEL","content":[{"type":"draft_task","draft_task":{"id":"DRAFT_TASK_ID"}}],"resolution":"1080p"}'`
            },
            {
              type: 'code',
              title: 'Query a task',
              code: `curl '{{BASE_URL}}/api/v3/contents/generations/tasks/{task_id}' \\
  -H 'Authorization: Bearer {{API_KEY}}'`
            },
            {
              type: 'code',
              title: 'Cancel a task',
              code: `curl -X DELETE '{{BASE_URL}}/api/v3/contents/generations/tasks/{task_id}' \\
  -H 'Authorization: Bearer {{API_KEY}}'`
            },
            {
              type: 'code',
              title: 'Download the result (use the exact URL from the response)',
              code: `curl '{{BASE_URL}}/v1/video-tasks/c2VlZGFuY2U6Y2d0LWV4YW1wbGUtdGFzay1pZA/content?index=0' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -o seedance-video.mp4`
            }
          ]
        },
        {
          id: 'seedance-responses',
          title: 'Response examples & status values',
          blocks: [
            {
              type: 'code',
              title: 'Submit response (Ark native format)',
              code: '{"id":"cgt-example-task-id"}'
            },
            {
              type: 'code',
              title: 'Query response (success example, upstream URL hidden)',
              code: '{"id":"cgt-example-task-id","model":"YOUR_VIDEO_MODEL","status":"succeeded","content":{"video_url":"/v1/video-tasks/c2VlZGFuY2U6Y2d0LWV4YW1wbGUtdGFzay1pZA/content?index=0"},"usage":{"completion_tokens":12345}}'
            },
            {
              type: 'p',
              text: 'The tasks page shows queued / running as "Processing", succeeded as "Completed", and failed / cancelled as their terminal states. Download with the exact result URL returned by the response — do not build it from the task ID.'
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
          title: 'Endpoints',
          blocks: [
            {
              type: 'table',
              headers: ['Method', 'Path', 'Description'],
              rows: [
                ['POST', '/v1beta/models/{model}:predictLongRunning', 'Create a task; the model is taken from the URL'],
                ['GET', '/v1beta/{operation_name}', 'Query a task; operation_name is the name from the submit response, e.g. models/veo-3.0-generate-001/operations/xxx']
              ]
            },
            { type: 'p', text: 'instances must contain exactly one object; the number of outputs is set via parameters.sampleCount. This site integrates the Gemini API (generativelanguage), so the Vertex AI limit of 1–4 results does not apply.' }
          ]
        },
        {
          id: 'gemini-params',
          title: 'Main parameters',
          blocks: [
            {
              type: 'table',
              headers: ['Field', 'Type', 'Required', 'Description'],
              rows: [
                ['instances', 'array', 'Yes', 'Exactly 1 object; prompt is the text; Veo 3.1 may use instances[0].referenceImages (up to 3 images, inlineData base64, requires 8s)'],
                ['parameters.sampleCount', 'integer', 'No', 'Number of outputs, default 1; veo-2.0 allows 1–2, other models are fixed at 1'],
                ['parameters.durationSeconds', 'integer', 'No', 'Default 8s; veo-2.0 allows 5–8s, veo-3.0 is 8s, veo-3.1 allows 4 / 6 / 8s'],
                ['parameters.resolution', 'string', 'No', 'Default 720p; 1080p on veo-3.0 is 16:9 only; 1080p or reference images on veo-3.1 require 8s'],
                ['parameters.aspectRatio', 'string', 'No', 'Default 16:9, submitted explicitly']
              ]
            },
            {
              type: 'p',
              text: 'The defaults above are the protocol defaults when no account capability is configured; omitted parameters are written into the upstream request explicitly by the gateway. With a capability configured: an unsupported default resolution falls back to the lowest tier the account supports, and an omitted duration is clamped to the account\'s effective range; set parameters explicitly when you need fixed output. 4K and video extension are rejected with 400 before submission. Actual duration/resolution availability follows the upstream account capabilities of your group — out-of-range values are rejected with 400 before submission, with the supported range in the message.'
            }
          ]
        },
        {
          id: 'gemini-examples',
          title: 'Examples',
          blocks: [
            {
              type: 'code',
              title: 'Text to video',
              code: `curl '{{BASE_URL}}/v1beta/models/YOUR_VIDEO_MODEL:predictLongRunning' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"instances":[{"prompt":"Waves under the setting sun"}],"parameters":{"durationSeconds":8,"resolution":"720p"}}'`
            },
            {
              type: 'code',
              title: 'Image to video (first frame)',
              code: `curl '{{BASE_URL}}/v1beta/models/YOUR_VIDEO_MODEL:predictLongRunning' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"instances":[{"prompt":"The camera pushes in slowly","image":{"inlineData":{"mimeType":"image/png","data":"<BASE64_IMAGE>"}}}],"parameters":{"durationSeconds":8,"resolution":"720p"}}'`
            },
            {
              type: 'code',
              title: 'Reference images (Veo 3.1 only, up to 3, requires 8s)',
              code: `curl '{{BASE_URL}}/v1beta/models/veo-3.1-generate-preview:predictLongRunning' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"instances":[{"prompt":"The person from the reference runs along the beach","referenceImages":[{"image":{"inlineData":{"mimeType":"image/png","data":"<BASE64_IMAGE>"}},"referenceType":"asset"}]}],"parameters":{"durationSeconds":8,"resolution":"720p"}}'`
            },
            {
              type: 'code',
              title: 'Query a task',
              code: `curl '{{BASE_URL}}/v1beta/models/veo-3.0-generate-001/operations/example-operation-id' \\
  -H 'Authorization: Bearer {{API_KEY}}'`
            },
            {
              type: 'code',
              title: 'Download the result (use the exact URL from the response)',
              code: `curl '{{BASE_URL}}/v1/video-tasks/Z2VtaW5pOm1vZGVscy92ZW8tMy4wLWdlbmVyYXRlLTAwMS9vcGVyYXRpb25zL2V4YW1wbGUtb3BlcmF0aW9uLWlk/content?index=0' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -o veo-video.mp4`
            }
          ]
        },
        {
          id: 'gemini-responses',
          title: 'Response examples & status values',
          blocks: [
            {
              type: 'code',
              title: 'Submit response',
              code: '{"name":"models/veo-3.0-generate-001/operations/example-operation-id"}'
            },
            {
              type: 'code',
              title: 'Query response (done example)',
              code: '{"name":"models/veo-3.0-generate-001/operations/example-operation-id","done":true,"response":{"generateVideoResponse":{"generatedSamples":[{"video":{"uri":"/v1/video-tasks/Z2VtaW5pOm1vZGVscy92ZW8tMy4wLWdlbmVyYXRlLTAwMS9vcGVyYXRpb25zL2V4YW1wbGUtb3BlcmF0aW9uLWlk/content?index=0"}}]}}}'
            },
            {
              type: 'p',
              text: 'done=false means processing; done=true means the operation has ended, but a valid generated video result is still required for success: an error field (Google format code / message / status) or empty output (no usable video) fails the task — done with no error alone does not mean the result is downloadable. Download with the exact result URL returned by the response — do not build it from the trailing segment of the operation name.'
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
          title: 'Endpoints',
          blocks: [
            {
              type: 'table',
              headers: ['Method', 'Path', 'Description'],
              rows: [
                ['POST', '/v2/video_generation', 'Create a task; also accepts legacy prompt / first/last-frame fields and converts them into content'],
                ['GET', '/v2/query/video_generation/{task_id}', 'Query a task by the task ID in the path']
              ]
            },
            { type: 'p', text: 'Each request must contain non-empty text; first/last frames (first_frame / last_frame) cannot be mixed with reference media. GET /v2/query/video_generation?task_id=... is the upstream task list, which the gateway does not expose.' }
          ]
        },
        {
          id: 'minimax-v2-params',
          title: 'Main parameters',
          blocks: [
            {
              type: 'table',
              headers: ['Field', 'Type', 'Required', 'Description'],
              rows: [
                ['model', 'string', 'Yes', 'MiniMax-H3 / MiniMax-H3-Max or the mapped name configured in the group'],
                ['content', 'array', 'Required for native v2', 'Content array, must contain non-empty text; image_url with role: first_frame / last_frame are first/last frames; role: reference_image is a reference image (up to 9, cannot be mixed with first/last frames). May be omitted when submitting with the compatible prompt / first/last-frame fields, which the gateway converts'],
                ['duration', 'integer', 'No', 'Default 6s; official ranges: 4–15s for H3, 5–15s for H3-Max'],
                ['resolution', 'string', 'No', 'Default 768P; H3 supports 768P / 2K, H3-Max supports 480P / 768P (no 2K)'],
                ['ratio', 'string', 'No', 'Required for text-to-video, e.g. 16:9; image-to-video may use adaptive']
              ]
            },
            {
              type: 'p',
              text: 'The defaults above are the protocol defaults when no account capability is configured. With a capability configured: an unsupported default resolution falls back to the lowest tier the account supports, and an omitted duration is clamped to the account\'s effective range; set parameters explicitly when you need fixed output. Actual duration/resolution availability follows the upstream account capabilities of your group (e.g. a metaso MiniMax-H3 account may be configured with a wider range) — out-of-range values are rejected with 400 before submission, with the supported range in the message.'
            }
          ]
        },
        {
          id: 'minimax-v2-examples',
          title: 'Examples',
          blocks: [
            {
              type: 'code',
              title: 'Text to video',
              code: `curl '{{BASE_URL}}/v2/video_generation' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"MiniMax-H3","content":[{"type":"text","text":"Waves under the setting sun"}],"duration":6,"resolution":"768P","ratio":"16:9"}'`
            },
            {
              type: 'code',
              title: 'Image to video (first frame)',
              code: `curl '{{BASE_URL}}/v2/video_generation' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"MiniMax-H3","content":[{"type":"text","text":"The camera pushes in slowly"},{"type":"image_url","image_url":{"url":"https://example.com/first-frame.png"},"role":"first_frame"}],"duration":6,"resolution":"768P","ratio":"adaptive"}'`
            },
            {
              type: 'code',
              title: 'Reference images (up to 9, cannot mix with first/last frames)',
              code: `curl '{{BASE_URL}}/v2/video_generation' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"MiniMax-H3","content":[{"type":"text","text":"The person from the reference runs along the beach"},{"type":"image_url","image_url":{"url":"https://example.com/reference.png"},"role":"reference_image"}],"duration":6,"resolution":"768P"}'`
            },
            {
              type: 'code',
              title: 'Query a task',
              code: `curl '{{BASE_URL}}/v2/query/video_generation/{task_id}' \\
  -H 'Authorization: Bearer {{API_KEY}}'`
            },
            {
              type: 'code',
              title: 'Download the result (use the exact URL from the response)',
              code: `curl '{{BASE_URL}}/v1/video-tasks/bWluaW1heDpleGFtcGxlLXRhc2staWQ/content?index=0' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -o minimax-video.mp4`
            }
          ]
        },
        {
          id: 'minimax-v2-responses',
          title: 'Response examples & status values',
          blocks: [
            {
              type: 'code',
              title: 'Submit response',
              code: '{"task_id":"example-task-id"}'
            },
            {
              type: 'code',
              title: 'Query response (success example, upstream URL hidden)',
              code: '{"task":{"id":"example-task-id","status":"succeeded","content":{"url":"/v1/video-tasks/bWluaW1heDpleGFtcGxlLXRhc2staWQ/content?index=0"}}}'
            },
            {
              type: 'p',
              text: 'Status values: queued / running are processing, succeeded is completed, failed / cancelled are the terminal states. The successful result comes from task.content.url; download with the exact result URL returned by the response — do not build it from the task ID.'
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
          title: 'Endpoints',
          blocks: [
            {
              type: 'table',
              headers: ['Method', 'Path', 'Description'],
              rows: [
                ['POST', '/v1/video_generation', 'Create a task; text-to-video and image-to-video share the entry'],
                ['GET', '/v1/query/video_generation?task_id={task_id}', 'Query task status'],
                ['GET', '/v1/files/retrieve?file_id={file_id}', 'Fetch file download information; file_id comes from the succeeded task response']
              ]
            },
            { type: 'p', text: 'Native fields such as first_frame_image, last_frame_image, subject_reference, and prompt_optimizer are passed through; only the model is rewritten per account mapping.' }
          ]
        },
        {
          id: 'minimax-v1-params',
          title: 'Main parameters',
          blocks: [
            {
              type: 'table',
              headers: ['Field', 'Type', 'Required', 'Description'],
              rows: [
                ['model', 'string', 'Yes', 'Public model name (the mapped name configured in the group)'],
                ['prompt', 'string', 'Required for text-to-video', 'Prompt text; for image-to-video provide at least one of prompt text or an input image (first_frame_image etc.); model-specific limits are validated upstream'],
                ['duration', 'integer', 'No', 'Default 6s; the 6 / 10s rule applies, model-specific limits are validated upstream'],
                ['resolution', 'string', 'No', 'When omitted the gateway submits 768P explicitly; 512P is billed at the 480P tier — specify the resolution your older model supports explicitly']
              ]
            },
            {
              type: 'p',
              text: 'The defaults above are the protocol defaults when no account capability is configured. With a capability configured: an unsupported default resolution falls back to the lowest tier the account supports, and an omitted duration is clamped to the account\'s effective range; set parameters explicitly when you need fixed output. Actual duration/resolution availability follows the upstream account capabilities of your group — out-of-range values are rejected with 400 before submission, with the supported range in the message.'
            }
          ]
        },
        {
          id: 'minimax-v1-examples',
          title: 'Examples',
          blocks: [
            {
              type: 'code',
              title: 'Text to video',
              code: `curl '{{BASE_URL}}/v1/video_generation' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"YOUR_VIDEO_MODEL","prompt":"Waves under the setting sun","duration":6,"resolution":"1080P"}'`
            },
            {
              type: 'code',
              title: 'Image to video (first frame)',
              code: `curl '{{BASE_URL}}/v1/video_generation' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -H 'Content-Type: application/json' \\
  -d '{"model":"YOUR_VIDEO_MODEL","prompt":"The camera pushes in slowly","first_frame_image":"https://example.com/first-frame.png","duration":6,"resolution":"768P"}'`
            },
            {
              type: 'code',
              title: 'Query a task',
              code: `curl '{{BASE_URL}}/v1/query/video_generation?task_id={task_id}' \\
  -H 'Authorization: Bearer {{API_KEY}}'`
            },
            {
              type: 'code',
              title: 'Fetch file download information',
              code: `curl '{{BASE_URL}}/v1/files/retrieve?file_id={file_id}' \\
  -H 'Authorization: Bearer {{API_KEY}}'`
            },
            {
              type: 'code',
              title: 'Download the result (use the exact URL from the response)',
              code: `curl '{{BASE_URL}}/v1/video-tasks/bWluaW1heDpleGFtcGxlLXRhc2staWQ/content?index=0' \\
  -H 'Authorization: Bearer {{API_KEY}}' \\
  -o hailuo-video.mp4`
            }
          ]
        },
        {
          id: 'minimax-v1-responses',
          title: 'Response examples & status values',
          blocks: [
            {
              type: 'code',
              title: 'Submit response',
              code: '{"task_id":"example-task-id","base_resp":{"status_code":0,"status_msg":""}}'
            },
            {
              type: 'p',
              text: 'Status values: Preparing / Queueing / Processing are processing, Success is completed, Fail is failed. At submission, a non-zero base_resp.status_code in an HTTP 200 response is treated as a creation failure and no generation fee is charged. Once a query confirms Success, the server fetches the file URL with the same account; a file-info failure after that does not reverse the settled generation fee, and retrying the query or download does not charge again.'
            }
          ]
        }
      ]
    }
  ],
  afterProtocols: [
    {
      id: 'validation',
      title: 'Request Validation Rules',
      blocks: [
        {
          type: 'list',
          items: [
            'The JSON body must not contain duplicate keys (including keys that become equal after unescaping) or multiple JSON values, and nesting is limited to 128 levels.',
            'Cross-protocol quantity/usage fields that are not supported (e.g. n or sampleCount on Seedance) are rejected with 400 before the upstream call; Seedance and MiniMax tasks always generate exactly 1 video.',
            'When duration or resolution is omitted, the gateway fills in explicit defaults (see the parameter tables above) so the upstream output matches the price contract.',
            'The actual charge never exceeds the hold; if the usage returned upstream exceeds the contract, settlement is capped at the hold amount.'
          ]
        }
      ]
    },
    {
      id: 'errors',
      title: 'Error Codes',
      blocks: [
        {
          type: 'table',
          headers: ['Status', 'Meaning'],
          rows: [
            ['400', 'Invalid parameters; no video price configured (video_pricing_not_configured, the type field of local Seedance / MiniMax errors); duration/resolution outside the upstream account capabilities (the supported range is included in the message)'],
            ['401', 'Unauthenticated or invalid API key'],
            ['402', 'Balance cannot cover the hold and the request was not sent upstream; can also be a 402 passed through from the upstream'],
            ['403', 'Balance ≤ 0 (or below the minimum reserved balance), or no permission'],
            ['404', 'Task does not exist or does not belong to the current key / user'],
            ['413', 'Request body exceeds the size limit'],
            ['429', 'Quota or rate limit exceeded'],
            ['502', 'Upstream error; only "Upstream request was not sent" means nothing was sent and the hold was released. Other timeouts or connection failures may have been accepted upstream — the hold is kept pending verification, so confirm the task status before deciding whether to resubmit'],
            ['503', 'No available account for the protocol in the group (missing, disabled or busy), e.g. "No eligible Seedance accounts"; the message lists the endpoints of the other protocols, so check that the endpoint matches the protocol of your group\'s accounts']
          ]
        },
        {
          type: 'p',
          text: 'Gateway-local errors: Gemini entries use the Google format (error.code / message / status); Seedance and MiniMax entries use error.type / message. Upstream-native errors are passed through in each vendor\'s native format and are not normalized: Seedance returns the Ark native structure (e.g. error.code / message); MiniMax keeps its native JSON structure (the v1 upstream may signal failure with HTTP 200 and a non-zero base_resp.status_code / status_msg; the gateway keeps the native JSON error body but may map such business failures to 502 or 429, so check both the HTTP status and the business fields), and only non-JSON 4xx / 5xx bodies are wrapped into the generic error.type / message format; Gemini upstream errors also keep their native structure.'
        }
      ]
    }
  ]
}

export default en
