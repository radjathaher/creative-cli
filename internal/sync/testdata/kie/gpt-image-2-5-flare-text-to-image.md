# GPT Image 2.5 Flare - Text to Image

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
      summary: GPT Image 2.5 Flare - Text to Image
      deprecated: false
      description: >-
        ## Create Task


        Use this endpoint to create a new text-to-image generation task.


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
      operationId: gpt-image-2-5-flare-text-to-image
      tags:
        - docs/en/Market/Image    Models/GPT Image
      parameters: []
      requestBody:
        content:
          application/json:
            schema:
              properties:
                model:
                  default: gpt-image-2-5-flare-text-to-image
                  description: >-
                    The model name used for generation. This field is required.
                    This endpoint must use the
                    `gpt-image-2-5-flare-text-to-image` model.
                  enum:
                    - gpt-image-2-5-flare-text-to-image
                  examples:
                    - gpt-image-2-5-flare-text-to-image
                  type: string
                  x-apidog-enum:
                    - description: ''
                      name: ''
                      value: gpt-image-2-5-flare-text-to-image
                callBackUrl:
                  description: >-
                    Callback URL for task completion notifications. Optional
                    parameter. If provided, the system will send a POST request
                    to this URL when the task completes, whether it succeeds or
                    fails. If omitted, no callback notification will be sent. To
                    ensure callback security, see [Webhook Verification
                    Guide](/common-api/webhook-verification).
                  examples:
                    - https://your-domain.com/api/callback
                  format: uri
                  type: string
                input:
                  description: Input parameters for the text-to-image task.
                  properties:
                    prompt:
                      description: Text prompt. Required, maximum 20,000 characters.
                      examples:
                        - >-
                          A cinematic night city poster with neon reflections on
                          a rainy street.
                      maxLength: 20000
                      minLength: 1
                      type: string
                    aspect_ratio:
                      description: >-
                        The aspect ratio of the generated image is set to auto
                        by default. The 27:16, 16:27, 9:8 and 8:9 aspect ratios
                        support 1K only. 2K and 4K are available for other
                        aspect ratios.
                      enum:
                        - auto
                        - '1:1'
                        - '3:2'
                        - '2:3'
                        - '4:3'
                        - '3:4'
                        - '16:9'
                        - '9:16'
                        - '21:9'
                        - '27:16'
                        - '16:27'
                        - '9:8'
                        - '8:9'
                      type: string
                      x-apidog-enum:
                        - description: ''
                          label: auto
                          value: auto
                        - description: ''
                          label: '1:1'
                          value: '1:1'
                        - description: ''
                          label: '3:2'
                          value: '3:2'
                        - description: ''
                          label: '2:3'
                          value: '2:3'
                        - description: ''
                          label: '4:3'
                          value: '4:3'
                        - description: ''
                          label: '3:4'
                          value: '3:4'
                        - description: ''
                          label: '16:9'
                          value: '16:9'
                        - description: ''
                          label: '9:16'
                          value: '9:16'
                        - description: ''
                          label: '21:9'
                          value: '21:9'
                        - description: ''
                          name: ''
                          value: '27:16'
                        - description: ''
                          name: ''
                          value: '16:27'
                        - description: ''
                          name: ''
                          value: '9:8'
                        - description: ''
                          name: ''
                          value: '8:9'
                    resolution:
                      description: >-
                        Image resolution. The background parameter is only
                        available at 1K.
                      enum:
                        - 1K
                        - 2K
                        - 4K
                      type: string
                      x-apidog-enum:
                        - description: ''
                          name: ''
                          value: 1K
                        - description: ''
                          name: ''
                          value: 2K
                        - description: ''
                          name: ''
                          value: 4K
                    background:
                      description: >-
                        Image background

                        Note: This parameter is only supported when the
                        resolution is 1K.
                      enum:
                        - transparent
                        - opaque
                        - auto
                      type: string
                      x-apidog-enum:
                        - description: ''
                          name: Transparency
                          value: transparent
                        - description: ''
                          name: Opaque
                          value: opaque
                        - description: ''
                          name: Automatically
                          value: auto
                  required:
                    - prompt
                  type: object
                  x-apidog-orders:
                    - prompt
                    - aspect_ratio
                    - resolution
                    - background
                  x-apidog-ignore-properties: []
              required:
                - model
                - input
              type: object
              x-apidog-orders:
                - model
                - callBackUrl
                - input
              x-apidog-ignore-properties: []
            example:
              model: gpt-image-2-5-flare-text-to-image
              callBackUrl: https://your-domain.com/api/callback
              input:
                prompt: >-
                  A cinematic night city poster with neon reflections on a rainy
                  street.
                aspect_ratio: '3:2'
                resolution: 1K
                background: transparent
      responses:
        '200':
          description: Request successful
          content:
            application/json:
              schema:
                allOf:
                  - properties:
                      code:
                        type: integer
                        description: >-
                          Status code of the request. The HTTP status is 200
                          even when the request fails, so always check this
                          field: 200 means success; any other value means the
                          request failed and msg describes the reason. Common
                          values: 401 invalid or missing API key; 402
                          insufficient credits; 429 rate limit exceeded; 422 or
                          500 the request could not be processed (read msg and
                          check your parameters before retrying).
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
                              The task ID can be used with the "Get Task
                              Details" endpoint to query the task status.
                            examples:
                              - dc1928bfcbc77cb6c85f3359a9c718b3
                        x-apidog-orders:
                          - taskId
                        x-apidog-ignore-properties: []
                    required:
                      - data
                    type: object
                    x-apidog-orders:
                      - 01KPR07RANE520VZS4M5X3TGNY
                    x-apidog-refs:
                      01KPR07RANE520VZS4M5X3TGNY:
                        $ref: '#/components/schemas/response%20not%20with%20recordId'
                    x-apidog-ignore-properties:
                      - code
                      - msg
                      - data
              example:
                code: 200
                msg: success
                data:
                  taskId: task_gptimage_1765180586443
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
        onImageGenerated:
          '{$request.body#/callBackUrl}':
            post:
              summary: Image Generation Callback
              description: Sent when the task succeeds or fails.
              requestBody:
                required: true
                content:
                  application/json:
                    schema:
                      type: object
                      properties:
                        code:
                          type: integer
                          description: >-
                            200: success. 501: failure; see data.failCode and
                            data.failMsg.
                          enum:
                            - 200
                            - 501
                        msg:
                          type: string
                          description: Status message
                          example: Playground task completed successfully.
                        data:
                          type: object
                          properties:
                            completeTime:
                              type: integer
                              format: int64
                              description: >-
                                Task completion time, represented as a Unix
                                timestamp in milliseconds
                              example: 1786432830000
                            costTime:
                              type: integer
                              description: >-
                                Processing time in seconds. Present on both
                                success and failure callbacks.
                              example: 83
                            createTime:
                              type: integer
                              format: int64
                              description: >-
                                Task creation time, represented as a Unix
                                timestamp in milliseconds
                              example: 1786432746000
                            creditsConsumed:
                              type: number
                              format: double
                              description: >-
                                Credits consumed by the task; 0 for failed
                                tasks.
                              example: 3
                            model:
                              type: string
                              description: Image generation model used for the task
                              example: gpt-image-2-5-flare-text-to-image
                            param:
                              type: string
                              description: >-
                                Parameters submitted when creating the task, in
                                JSON string format
                              example: >-
                                {"input":"{\"aspect_ratio\":\"auto\",\"prompt\":\"A
                                cinematic night city poster with neon
                                reflections on a rainy
                                street.\"}","callBackUrl":"https://your-domain.com/api/callback","model":"gpt-image-2-5-flare-text-to-image"}
                            resultJson:
                              type: string
                              description: >-
                                Image generation result in JSON string format.
                                resultUrls contains the list of generated image
                                URLs. Present only when state is success.
                              example: >-
                                {"resultUrls":["https://tempfile.aiquickdraw.com/images/chatgpt/file_000000002b24820caef9f1684a4f4747.png"]}
                            state:
                              type: string
                              description: Task status
                              enum:
                                - success
                                - fail
                              example: success
                            taskId:
                              type: string
                              description: Task ID
                              example: ed578231bfed231f0884c564579a965d
                            updateTime:
                              type: integer
                              format: int64
                              description: >-
                                Use completeTime as the completion time;
                                updateTime may equal createTime.
                              example: 1786432830000
                            failCode:
                              type: string
                              description: >-
                                Failure code, a numeric string such as "400" or
                                "500". Present only when state is fail.
                              example: '500'
                            failMsg:
                              type: string
                              description: Failure reason. Present only when state is fail.
                              example: <error detail>
                    examples:
                      success:
                        summary: Success
                        value:
                          code: 200
                          msg: Playground task completed successfully.
                          data:
                            taskId: ed578231bfed231f0884c564579a965d
                            model: gpt-image-2-5-flare-text-to-image
                            state: success
                            param: >-
                              {"input":"{\"aspect_ratio\":\"auto\",\"prompt\":\"A
                              cinematic night city poster with neon reflections
                              on a rainy
                              street.\"}","callBackUrl":"https://your-domain.com/api/callback","model":"gpt-image-2-5-flare-text-to-image"}
                            resultJson: >-
                              {"resultUrls":["https://tempfile.aiquickdraw.com/images/chatgpt/file_000000002b24820caef9f1684a4f4747.png"]}
                            completeTime: 1786432830000
                            createTime: 1786432746000
                            costTime: 83
                            creditsConsumed: 3
                            updateTime: 1786432830000
                      failure:
                        summary: Failure
                        value:
                          code: 501
                          msg: Playground task failed.
                          data:
                            taskId: ed578231bfed231f0884c564579a965d
                            model: gpt-image-2-5-flare-text-to-image
                            state: fail
                            param: >-
                              {"input":"{\"aspect_ratio\":\"auto\",\"prompt\":\"A
                              cinematic night city poster with neon reflections
                              on a rainy
                              street.\"}","callBackUrl":"https://your-domain.com/api/callback","model":"gpt-image-2-5-flare-text-to-image"}
                            failCode: '500'
                            failMsg: <error detail>
                            completeTime: 1786432830000
                            createTime: 1786432746000
                            costTime: 83
                            creditsConsumed: 0
                            updateTime: 1786432830000
              responses:
                '200':
                  description: Callback received successfully
      x-apidog-folder: docs/en/Market/Image    Models/GPT Image
      x-apidog-status: released
      x-run-in-apidog: https://app.apidog.com/web/project/1184766/apis/api-43283988-run
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
