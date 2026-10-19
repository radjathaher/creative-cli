# Gemini Omni 1.1 Flash

## OpenAPI Specification

```yaml
openapi: 3.0.1
info:
  title: ''
  description: ''
  version: 1.0.0
paths:
  /api/v1/jobs/createTask:
    post:
      summary: Gemini Omni 1.1 Flash
      deprecated: false
      description: >
        ## Create Task


        Use this endpoint to create a new multimodal video generation task.


        <Card title="Get Task Details" icon="lucide-search"
        href="/market/common/get-task-detail">
          After submission, use the unified query endpoint to check task progress and retrieve results
        </Card>


        ::: tip[]

        For production use, we recommend providing the `callBackUrl` parameter
        so your service can receive completion notifications instead of polling
        for task status.

        :::


        ## Related Resources


        <CardGroup cols={2}>
          <Card title="Model Marketplace" icon="lucide-store" href="/market/quickstart">
            Explore all available models and capabilities
          </Card>
          <Card title="Common API" icon="lucide-cog" href="/common-api/get-account-credits">
            Check account credits and usage
          </Card>
        </CardGroup>


        **Total Quota Overview:**

        The system utilizes a quota-based system for content uploads. The total
        available capacity is **7 units**. Different parameters consume
        different amounts of this quota. Please ensure the sum of all consumed
        units does not exceed 7.


        **Quota Consumption Rules:**

        1.  **Images (`image_urls`)**: Each image consumes **1 unit**.

        2.  **Videos (`video_list`)**: Each video consumes **2 units**.
            *   *Note: Maximum of 1 video per request.*
        3.  **Character IDs (`character_ids`)**: Each ID consumes **1 unit**.
            *   *Note: Maximum of 3 character IDs per request.*

        **Constraint Formula:**

        `(Number of Images) + (Number of Videos × 2) + (Number of Character IDs)
        ≤ 7`


        **Example Scenarios:**

        *   If 1 video (2 units) and 3 Character IDs (3 units) are uploaded, you
        can upload a maximum of 2 additional images.

        *   If no video or Character IDs are provided, you can upload up to 7
        images.


        **Note — first-frame mode:** when first_frame_url is set, image_urls,
        video_list, character_ids and audio_ids cannot be used.
      operationId: gemini-omni-1.1-flash
      tags:
        - docs/en/Market/Video Models/Gemini Omni
      parameters: []
      requestBody:
        content:
          application/json:
            schema:
              type: object
              required:
                - model
                - input
              properties:
                model:
                  type: string
                  enum:
                    - google/gemini-omni-flash-1-1
                  default: google/gemini-omni-flash-1-1
                  description: >-
                    The model name used to generate the task. This field is
                    required. This interface must use the
                    `google/gemini-omni-flash-1-1` model.
                  examples:
                    - google/gemini-omni-flash-1-1
                callBackUrl:
                  type: string
                  format: uri
                  description: >-
                    The callback URL for task completion notifications. This
                    parameter is optional. If provided, the system will send a
                    POST request to this URL when the task is completed,
                    regardless of whether it succeeds or fails. If not provided,
                    no callback notification will be sent.
                  examples:
                    - https://your-domain.com/api/callback
                input:
                  type: object
                  description: Input parameters for the multimodal video generation task.
                  properties:
                    prompt:
                      type: string
                      description: >-
                        The video prompt, used to describe the visual content,
                        style, cinematography, or character actions of the
                        target video.
                      examples:
                        - >-
                          Generate a short video of a futuristic city at night,
                          with the camera slowly pushing forward as a character
                          emerges from a neon-lit street.
                      maxLength: 20000
                    image_urls:
                      type: array
                      items:
                        type: string
                        format: uri
                      description: >-
                        An array of image URLs. Multiple reference images can be
                        uploaded to provide references for characters, scenes,
                        styles, or storyboards.


                        Image limitations:

                        - Each file must not exceed `20MB`

                        - Use publicly accessible image URLs

                        - Maximum of 7 images

                        - Note: This field is mutually exclusive with the
                        first-frame image (`first_frame_url`) and cannot be
                        provided at the same time.
                      examples:
                        - - https://example.com/assets/scene-1.png
                          - https://example.com/assets/scene-2.png
                    first_frame_url:
                      type: string
                      format: uri
                      description: >-
                        The URL of the first-frame image. Used to specify the
                        starting frame of the generated video. Note: This field
                        is mutually exclusive with `image_urls`, `audio_ids`,
                        `video_list`, and `character_ids` and cannot be provided
                        at the same time.
                      examples:
                        - https://example.com/assets/first-frame.png
                    last_frame_url:
                      type: string
                      format: uri
                      description: >-
                        The URL of the last-frame image. Used to specify the
                        ending frame of the generated video. Note: The
                        last-frame image cannot be used alone and must be
                        provided together with the first-frame image
                        (`first_frame_url`).
                      examples:
                        - https://example.com/assets/last-frame.png
                    audio_ids:
                      type: array
                      items:
                        type: string
                      description: >-
                        An array of audio IDs generated by the
                        `gemini-omni-audio` interface. Used to provide
                        narration, dialogue, background music, or sound
                        references for the video. Up to 3 items. Note: This
                        field is mutually exclusive with the first-frame image
                        (`first_frame_url`) and cannot be provided at the same
                        time.
                      examples:
                        - - audio_01hx8p0demo
                    video_list:
                      type: array
                      items:
                        type: object
                        required:
                          - url
                          - start
                          - ends
                        properties:
                          url:
                            type: string
                            format: uri
                            description: >-
                              The video URL. Each video file must not exceed
                              `100MB`, and the video duration must not exceed
                              `30s`.
                          start:
                            type: number
                            minimum: 0
                            description: The start time, in seconds.
                          ends:
                            type: number
                            minimum: 0
                            description: >-
                              The end time, in seconds. It should be greater
                              than `start`. The difference between the end and
                              start times must not exceed 10 seconds.
                        x-apidog-orders:
                          - url
                          - start
                          - ends
                        x-apidog-ignore-properties: []
                      description: >-
                        An array of video clips. Each element describes a video
                        asset that can be used for generation and its trimming
                        time range.


                        Video limitations:

                        - Each file must not exceed `100MB`

                        - Video duration must not exceed `30s`

                        - `ends` should be greater than `start`

                        - The difference between the end and start times must
                        not exceed `10s`

                        - Maximum of 1 item, occupying two image slots

                        - Note: This field is mutually exclusive with the
                        first-frame image (`first_frame_url`) and cannot be
                        provided at the same time.
                      examples:
                        - - url: https://example.com/assets/source-video.mp4
                            start: 0
                            ends: 10
                    character_ids:
                      type: array
                      items:
                        type: string
                      description: >-
                        An array of character IDs generated by the
                        `gemini-omni-character` interface. Used to provide
                        character appearance, identity, or person references for
                        the video. Up to 3 character IDs. Each uses 1 image slot
                        (2 if created with portrait + body images). Total slots
                        (images + videos × 2 + character slots) ≤ 7. Note: This
                        field is mutually exclusive with the first-frame image
                        (`first_frame_url`) and cannot be provided at the same
                        time.
                      examples:
                        - - character_01hx8p0demo
                    duration:
                      type: string
                      enum:
                        - '4'
                        - '6'
                        - '8'
                        - '10'
                      description: >-
                        The duration of the generated video, in seconds. Valid
                        values are 4, 6, 8, and 10.

                        This parameter only takes effect when there is no video
                        input. If video input is used, the output duration is
                        determined by the model, and the `duration` parameter
                        will be ignored. Note: When video input is provided, the
                        output duration is automatically determined by the
                        model. In this case, the `duration` parameter will not
                        take effect.
                      examples:
                        - '8'
                    aspect_ratio:
                      type: string
                      enum:
                        - '16:9'
                        - '9:16'
                      description: >-
                        The aspect ratio of the generated video. `16:9` is
                        landscape, and `9:16` is portrait.
                      examples:
                        - '16:9'
                    resolution:
                      type: string
                      enum:
                        - 360p
                        - 720p
                        - 1080p
                        - 4k
                      description: >-
                        The resolution of the generated video. Valid values are
                        360p, 720p, 1080p, and 4k.
                      default: 720p
                      examples:
                        - 720p
                    seed:
                      type: integer
                      description: >-
                        The random seed. Valid range: [0, 2147483647]. If not
                        specified, the system will automatically generate a
                        seed. A fixed seed can improve result reproducibility,
                        but because the model itself is stochastic, results may
                        still vary.
                  x-apidog-orders:
                    - prompt
                    - image_urls
                    - first_frame_url
                    - last_frame_url
                    - audio_ids
                    - video_list
                    - character_ids
                    - duration
                    - aspect_ratio
                    - resolution
                    - seed
                  required:
                    - prompt
                    - duration
                  dependencies:
                    last_frame_url:
                      - first_frame_url
                  x-apidog-ignore-properties: []
              x-apidog-orders:
                - model
                - callBackUrl
                - input
              x-apidog-ignore-properties: []
            example:
              model: google/gemini-omni-flash-1-1
              callBackUrl: https://your-domain.com/api/callback
              input:
                prompt: >-
                  Create a futuristic night city short film with a slow push-in
                  shot as the character walks out from a neon-lit street.
                image_urls:
                  - https://example.com/assets/scene-1.png
                  - https://example.com/assets/scene-2.png
                audio_ids:
                  - audio_01hx8p0demo
                video_list:
                  - url: https://example.com/assets/source-video.mp4
                    start: 0
                    ends: 10
                duration: '4'
      responses:
        '200':
          description: Request successful
          content:
            application/json:
              schema:
                allOf:
                  - $ref: '#/components/schemas/response%20not%20with%20recordId'
                  - type: object
                    properties:
                      data:
                        type: object
                        properties:
                          taskId:
                            type: string
                            description: >-
                              Task ID, which can be used to query task status
                              through the task detail endpoint.
                            examples:
                              - task_gemini_1765180586443
                        x-apidog-orders:
                          - taskId
                        x-apidog-ignore-properties: []
                    x-apidog-orders:
                      - data
                    x-apidog-ignore-properties: []
              example:
                code: 200
                msg: success
                data:
                  taskId: task_gemini_1765180586443
          headers: {}
          x-apidog-name: ''
      security:
        - BearerAuth: []
          x-apidog:
            schemeGroups:
              - id: kn8M4YUlc5i0A0179ezwx
                schemeIds:
                  - BearerAuth
            required: true
            use:
              id: kn8M4YUlc5i0A0179ezwx
            scopes:
              kn8M4YUlc5i0A0179ezwx:
                BearerAuth: []
      callbacks:
        videoTaskCompleted:
          '{request.body#/callBackUrl}':
            post:
              description: >-
                The system sends this callback when the
                `google/gemini-omni-flash-1-1` task succeeds or fails.
              requestBody:
                required: true
                content:
                  application/json:
                    schema:
                      type: object
                      required:
                        - code
                        - msg
                        - data
                      properties:
                        code:
                          type: integer
                          description: >-
                            Unified callback code: 200 for success, 501 for
                            failure.
                          enum:
                            - 200
                            - 501
                        msg:
                          type: string
                          description: Unified callback message.
                          enum:
                            - Playground task completed successfully.
                            - Playground task failed.
                        data:
                          type: object
                          required:
                            - taskId
                            - model
                            - state
                            - param
                          properties:
                            taskId:
                              type: string
                              description: The unique task identifier.
                            model:
                              type: string
                              description: The model used for the task.
                              enum:
                                - google/gemini-omni-flash-1-1
                            state:
                              type: string
                              description: Terminal task state.
                              enum:
                                - success
                                - fail
                            param:
                              type: string
                              description: >-
                                JSON string containing the submitted task
                                parameters.
                            resultJson:
                              type: string
                              nullable: true
                              description: >-
                                JSON string containing resultUrls,
                                firstFrameUrl, lastFrameUrl, or resultObject
                                when successful. Present only when state is
                                success.
                            failCode:
                              type: string
                              nullable: true
                              description: >-
                                Failure code, a numeric string such as "400" or
                                "500". Present only when state is fail.
                            failMsg:
                              type: string
                              nullable: true
                              description: Failure reason. Present only when state is fail.
                            costTime:
                              type: integer
                              format: int64
                              description: >-
                                Processing time in seconds. Present on both
                                success and failure callbacks.
                            completeTime:
                              type: integer
                              format: int64
                              description: Task completion timestamp.
                            createTime:
                              type: integer
                              format: int64
                              description: Task creation timestamp.
                            updateTime:
                              type: integer
                              format: int64
                              description: >-
                                Use completeTime as the completion time;
                                updateTime may equal createTime.
                            creditsConsumed:
                              type: number
                              description: >-
                                Credits consumed by the task; 0 for failed
                                tasks.
                    examples:
                      success:
                        summary: Task completed successfully
                        value:
                          code: 200
                          msg: Playground task completed successfully.
                          data:
                            taskId: task_example_video_webhook_001
                            model: google/gemini-omni-flash-1-1
                            state: success
                            param: >-
                              {"input":"{\"prompt\":\"Create a futuristic night
                              city short film with a slow push-in shot as the
                              character walks out from a neon-lit
                              street.\",\"image_urls\":[\"https://example.com/input-image.jpg\"],\"audio_ids\":[\"audio_01hx8p0demo\"],\"video_list\":[{\"url\":\"https://example.com/input-video.mp4\",\"start\":0,\"ends\":10}],\"duration\":\"4\"}","callBackUrl":"https://example.com/callback","model":"google/gemini-omni-flash-1-1"}
                            resultJson: >-
                              {"resultUrls":["https://example.com/generated-video.mp4"]}
                            costTime: 12
                            completeTime: 1234567890
                            createTime: 1234560000
                            updateTime: 1234567890
                            creditsConsumed: 1.23
                      failure:
                        summary: Task failed
                        value:
                          code: 501
                          msg: Playground task failed.
                          data:
                            taskId: task_example_video_webhook_001
                            model: google/gemini-omni-flash-1-1
                            state: fail
                            param: >-
                              {"input":"{\"prompt\":\"Create a futuristic night
                              city short film with a slow push-in shot as the
                              character walks out from a neon-lit
                              street.\",\"image_urls\":[\"https://example.com/input-image.jpg\"],\"audio_ids\":[\"audio_01hx8p0demo\"],\"video_list\":[{\"url\":\"https://example.com/input-video.mp4\",\"start\":0,\"ends\":10}],\"duration\":\"4\"}","callBackUrl":"https://example.com/callback","model":"google/gemini-omni-flash-1-1"}
                            failCode: '400'
                            failMsg: <error detail>
                            costTime: 12
                            creditsConsumed: 0
              responses:
                '200':
                  description: Callback received successfully.
                  content:
                    application/json:
                      schema:
                        type: object
                        properties:
                          code:
                            type: integer
                            example: 200
                          msg:
                            type: string
                            example: success
                      example:
                        code: 200
                        msg: success
      x-apidog-folder: docs/en/Market/Video Models/Gemini Omni
      x-apidog-status: released
      x-run-in-apidog: https://app.apidog.com/web/project/1184766/apis/api-42530918-run
components:
  schemas:
    response not with recordId:
      type: object
      required:
        - data
      properties:
        code:
          type: integer
          description: >-
            Status code of the request. The HTTP status is 200 even when the
            request fails, so always check this field: 200 means success; any
            other value means the request failed and msg describes the reason.
            Common values: 401 invalid or missing API key; 402 insufficient
            credits; 429 rate limit exceeded; 422 or 500 the request could not
            be processed (read msg and check your parameters before retrying).
          enum:
            - 200
            - 401
            - 402
            - 404
            - 422
            - 429
            - 433
            - 455
            - 500
            - 501
            - 505
        msg:
          type: string
          description: Response message, error description upon failure
          examples:
            - success
        data:
          type: object
          required:
            - taskId
          properties:
            taskId:
              type: string
              description: >-
                The task ID can be used with the "Get Task Details" endpoint to
                query the task status.
              examples:
                - dc1928bfcbc77cb6c85f3359a9c718b3
          x-apidog-orders:
            - taskId
          x-apidog-ignore-properties: []
      x-apidog-orders:
        - code
        - msg
        - data
      x-apidog-ignore-properties: []
      x-apidog-folder: ''
  securitySchemes:
    BearerAuth:
      type: bearer
      scheme: bearer
      bearerFormat: API Key
      description: >-
        All API requests require a Bearer Token. Add the header `Authorization:
        Bearer YOUR_API_KEY` to authenticate requests.
    BearerAuth1:
      type: bearer
      scheme: bearer
      bearerFormat: API Key
      description: >-
        所有 API 请求都需要 Bearer Token。请在请求头中添加 `Authorization: Bearer YOUR_API_KEY`
        进行身份验证。
servers:
  - url: https://api.kie.ai
    description: 正式环境
security:
  - BearerAuth: []
    x-apidog:
      schemeGroups:
        - id: kn8M4YUlc5i0A0179ezwx
          schemeIds:
            - BearerAuth
      required: true
      use:
        id: kn8M4YUlc5i0A0179ezwx
      scopes:
        kn8M4YUlc5i0A0179ezwx:
          BearerAuth: []

```
