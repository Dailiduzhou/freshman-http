# 从`HTTP`开始的后端学习之路

## `HTTP`概念介绍。

**超文本传输协议**（英语：HyperText Transfer Protocol，缩写：`HTTP`）是一种用于分布式、协作式和超媒体信息系统的应用层协议。HTTP是万维网的数据通信的基础。

![HTTP](images/HTTP_logo.webp)

虽然概念很复杂，但`HTTP`做的事情很简单。
它定义了一整套网络传输的协议。规定了互联网中网络设备的打招呼、信息传递、结束会话的方法。使得用户访问网站，前端向后端发送请求等交互，变得可能。

![HTTP](images/cs-structure.png)

`HTTP` 是**应用层**协议：它只规定“报文长什么样”，真正把字节送到对端的是底下那一层 —— `HTTP` 跑在 `TCP` 之上，`HTTPS` 还要在中间多一层 `TLS` 加密。

这一层不用我们操心：操作系统和 Go 的标准库把建连接、握手、重传都做完了。但记住**它存在**很重要：后面你会遇到“连接超时”“连接池复用”，也会遇到 `TLS` 握手失败 —— 这些词说的都是这一层，而不是 `HTTP` 这一层。

接下来，我们将深入以下概念：

- [`URL`](#url)
- [`HTTP`状态码](#http状态码)
- [`HTTP`请求方法](#http请求方法)
- [`HTTP`请求与响应细节](#http-请求和响应细节)
  - 请求头 / 请求体
  - 响应头 / 响应体
- [HTTP 是无状态的](#http-是无状态的cookiesessiontoken)
- [一次完整的 `GET` 请求之旅](#一次完整的-get-请求之旅)
- [`net/http` 三件套](#nethttp-三件套)
- [动手试试](#动手试试)
- [接下来去哪](#接下来去哪)

## `URL`

**统一资源定位符**（英语：Uniform Resource Locator，缩写：`URL`，或称统一资源定位器、定位地址、URL地址）俗称网页地址，简称网址，是因特网上标准的资源的地址（Address），如同在网络上的门牌。

`URL`的标准格式如下：
```
[协议类型]://[服务器地址]:[端口号]/[资源层级UNIX文件路径][文件名]?[查询]#[片段ID]
```

![URL示例](images/URL_example.png)

- 协议类型：`HTTPS`是`HTTP`的安全版本。它在`HTTP`和`TCP`之间加了一层`TLS`，解决两件事：**别人看不到你传的内容**（加密），以及**你连的确实是那台服务器**（身份验证）。代价是多一次握手，所以会慢一点点。除此之外，`HTTPS`和`HTTP`的报文格式、状态码、请求方法**完全一样** —— 你在这个文档里学到的东西，换成`HTTPS`一个字都不用改。
- 服务器地址：这里的`www.bilibili.com`是域名。它也可以是`192.168.1.1`这样的`IP`地址。
- 端口号：`80`是默认端口号，`443`是`HTTPS`的默认端口号。在这里并没有体现。
- 路径：`/`表示根目录，后面的`path`表示资源的路径。此处是`/video/BV1f6p4zJEZj`。
- 查询参数（query）：`?`后面的`spm_id_from=333.1387.upload.video_card.click`是查询参数。
- 片段（fragment）：`#`后面的部分。它**不会发给服务器**，只由浏览器自己使用（比如跳到页面里的某个锚点）—— 这也是本文档开头那些目录链接的原理。

无论是在互联网上获取资源，还是开发高可用性的后端服务，都离不开`URL`。

## `HTTP`状态码

**HTTP状态码**（英语：HTTP Status Code）是用以表示网页服务器超文本传输协议响应状态的3位数字代码，附带可选的英文消息短语。

它有5个类目。
| 格式 | 类别 | 内涵 |
| --- | --- | --- |
| 1xx | 信息响应 | 请求已接收，正在继续处理 |
| 2xx | 成功 | 请求已成功接收、理解并接受 |
| 3xx | 重定向 | 需要到新的地址再发一次请求，才能拿到真正的资源 |
| 4xx | 客户端错误 | 请求包含语法错误或无法完成请求 |
| 5xx | 服务器错误 | 服务器在处理请求的过程中发生了错误 |

我们以一个笑话来感性地认识一下HTTP状态码。

> 一个 HTTP 请求去相亲。
>
> 他问姑娘：“你愿意和我交往吗？”
>
> 姑娘返回：**401 Unauthorized**——“请先身份认证。”
>
> 请求赶紧带上 Cookie 和 Token，再问一次。
>
> 姑娘返回：**403 Forbidden**——“认证过了，但你没权限。”
>
> 请求不死心：“那先从朋友做起？”
>
> 姑娘返回：**301 Moved Permanently**——“我已经永久跳转到别人那儿了。”
>
> 请求崩溃：“为什么？”
>
> 姑娘返回：**418 I'm a teapot**——“因为我是个茶壶，不谈恋爱。”
>
> 请求只好返回 **404 Not Found**——心找不到了。

HTTP状态码对我们完成客户端-服务端的交互起到关键的作用。

上面那张表说的是“5 个类目”，但真正写代码的时候，你需要的是一张更具体的速查表 —— 因为你在 Go 里判断成败，靠的就是 `resp.StatusCode` 这个 `int`：

| 状态码 | 含义 | 写客户端时你会怎么做 |
| --- | --- | --- |
| `200 OK` | 成功 | 正常读响应体 |
| `201 Created` | 创建成功（`POST` 的典型返回） | 读响应体拿新资源的 id，同时看一眼 `Location` 头 —— 它指向新资源自己的地址 |
| `204 No Content` | 成功，但**没有响应体** | 别去读 body，读到的是空字节 |
| `301` / `302` | 永久 / 临时重定向 | `http.Client` 会**自动跟随**，一般不用管 |
| `400 Bad Request` | 请求本身有问题（参数格式、`JSON` 语法） | 检查你发出去的数据，重试没用 |
| `401 Unauthorized` | 没认证，或者凭证过期了 | 补上 `Authorization` 头 |
| `403 Forbidden` | 认证过了，但没权限 | 换账号或找管理员，重试没用 |
| `404 Not Found` | 资源不存在 | 检查 `URL` 拼写 |
| `429 Too Many Requests` | 被限流了 | 看 `Retry-After` 头，等一会儿再试 |
| `500 Internal Server Error` | 服务端自己出错了 | 稍后重试可能有救 |
| `502` / `503` | 网关 / 服务不可用（常见于发版、过载） | 同上，适合退避重试 |

有两点反直觉，值得单独拎出来说。

第一，**`3xx` 是会被自动跟随的**。`http.Client` 默认最多自动跳 10 次 —— 所以你请求 A 地址，最后拿到的可能是 B 地址的响应，`err` 还是 `nil`，你根本不会察觉。想知道真正落到哪里，得看 `resp.Request.URL`。

第二，**`429` 和 `503` 通常会在响应头里带一个 `Retry-After`**，告诉你要等多久再试。这也是后面 `src/usersync` 里的重试逻辑会去读的字段。

## `HTTP`请求方法

| 序号 | 方法 | 描述 |
| --- | --- | --- |
| 1 | GET | 从服务器获取资源。用于请求数据而不对数据进行更改。例如，从服务器获取网页、图片等。 |
| 2 | POST | 向服务器发送数据以创建新资源。常用于提交表单数据或上传文件。发送的数据包含在请求体中。 |
| 3 | PUT | 向服务器发送数据以更新现有资源。如果资源不存在，则创建新的资源。与 POST 不同，PUT 通常是幂等的，即多次执行相同的 PUT 请求不会产生不同的结果。 |
| 4 | DELETE | 从服务器删除指定的资源。请求中包含要删除的资源标识符。 |
| 5 | PATCH | 对资源进行部分修改。与 PUT 类似，但 PATCH 只更改部分数据而不是替换整个资源。 |
| 6 | HEAD | 类似于 GET，但服务器只返回响应的头部，不返回实际数据。用于检查资源的元数据（例如，检查资源是否存在，查看响应的头部信息）。 |
| 7 | OPTIONS | 返回服务器支持的 HTTP 方法。用于检查服务器支持哪些请求方法，通常用于跨域资源共享（CORS）的预检请求。 |
| 8 | TRACE | 回显服务器收到的请求，主要用于诊断。客户端可以查看请求在服务器中的处理路径。 |
| 9 | CONNECT | 建立一个到服务器的隧道，通常用于 HTTPS 连接。客户端可以通过该隧道发送加密的数据。 |

这 9 个方法里，日常真正会用到的是前 5 个；`HEAD` / `OPTIONS` / `TRACE` / `CONNECT` 更多是工具和框架在用。

我们还可以按`F12`打开开发者工具，在`网络`的页面，查看请求。

![GET请求](images/get.png)

![GET请求交互过程](images/GET_请求交互.png)

当我们访问网站主页的时候，浏览器往往会发送一个`GET`请求，以获取图形化`HTML`页面。

![POST请求](images/post.png)

![POST请求交互过程](images/POST_请求交互1.png)

![POST请求交互过程](images/POST_请求交互2.png)

当我们在网页提交表单、进行账号注册、登录时，会向网站后端发送一个`POST`请求，向后端提交信息(信息的格式一般是`JSON`)。

在这里我们简要地介绍一下`JSON`。

> 什么是 JSON ？
>
>  - JSON 指的是 JavaScript 对象表示法（JavaScript Object Notation）
>  - JSON 是轻量级的文本数据交换格式
>  - JSON 独立于语言：JSON 使用 Javascript语法来描述数据对象，但是 JSON 仍然独立于语言和平台。JSON 解析器和 JSON 库支持许多不同的编程语言。 目前非常多的动态（PHP，JSP，.NET）编程语言都支持 JSON
>  - JSON 具有自我描述性，更易理解

示例如下：
```json
{
    "sites": [
    { "name":"菜鸟教程" , "url":"www.runoob.com" }, 
    { "name":"google" , "url":"www.google.com" }, 
    { "name":"微博" , "url":"www.weibo.com" }
    ]
}
```

当然除了**浏览器**之外，我们还能使用`Postman`, `APIfox`等请求发送，调试工具或者使用Go语言的`net/http`包发送HTTP请求。

在我们正式发送请求之前，我们还需要了解一些关于请求和响应的细节。

## `HTTP` 请求和响应细节

![HTTP请求报文构成](images/HTTP请求报文.png)

![HTTP响应报文构成](images/HTTP响应报文.png)

**请求首部字段**和**响应首部字段**，简称分别为**请求头**和**响应头**。

在这其中，比较重要的两个标头字段分别为`Content-Type`和`Content-Length`。
代表着请求/响应的内容类型和长度。在我们使用代码发送请求的时候，非常有用。

同时，在有些需要确认身份和做权限控制的网站，例如区分普通用户和管理员用户的网站，往往需要用户在访问的时候，在**请求头**带上token。

## HTTP 是无状态的：`Cookie`、`Session`、`Token`

上面那句“访问时要在请求头带上 token”，背后其实是一个很重要的性质。先说结论：**`HTTP` 本身是无状态的（stateless）**。

意思是：服务器处理你的两个请求时，**不会记得**这两个请求来自同一个人。它不保存“你来过”这件事。这样设计的好处是服务器可以随便扩到很多台，哪台接你的请求都行；代价是 —— 它没法自己认出你。

那“登录一次、之后一直保持登录”又是怎么做到的？答案是把身份信息**在每次请求里都带上**。有三种常见做法：

1. **`Cookie`**：服务器在响应头里用 `Set-Cookie` 塞一小段数据给浏览器，浏览器存下来，之后的每次请求都在请求头 `Cookie` 里带回去。它是“浏览器帮你**自动携带**”的机制。
2. **`Session`**：服务端建一份会话数据存在自己那边（内存或 `Redis`），只把一个随机的 `session_id` 通过 `Cookie` 交给客户端。客户端拿到的只是一把“钥匙”，真正的内容在服务端 —— 所以**状态在服务端**。
3. **`Token`**：服务端签一个凭证（比如 `JWT`）直接发给客户端，客户端自己保存，之后放在请求头 `Authorization` 里带上。服务端靠**验签**认出你，不用在服务端存会话 —— 所以**状态在客户端**，更适合有多个服务、或者客户端根本不是浏览器（比如你用 Go 写的程序）的场景。

一句话记法：`Cookie` 是**运输方式**，`Session` 和 `Token` 是**两种“我记住你是谁”的方案**；`Session` 把状态放在服务端，`Token` 把状态放在客户端。

这也就解释了为什么**用 Go 写客户端要手动管 `Cookie` 和 `Authorization` 头**：浏览器天生带着一个存 `Cookie` 的容器，而 `http.Client` 默认是“什么都不记”的（下面 `CookieJar` 那一节会讲怎么让它记住）。

## 一次完整的 `GET` 请求之旅

前面讲的都是“零件”，现在先把整条路线走一遍。**这一步不用看懂每个词**，后面会逐个拆开讲。

下面这 5 步就是 `src/naive-get` 做的全部事情，也是这个仓库里所有代码的骨架：

1. **造请求**：`http.NewRequest(method, url, body)` 得到一个 `*http.Request`。这一步**没有联网**，只是把“报文”在内存里拼好。
2. **发出去**：`client.Do(req)` 真正建连接、发报文、等响应，返回 `*http.Response`。
3. **立刻 `defer resp.Body.Close()`**：`resp.Body` 是连接上的**数据流**，不是已经读到内存的字节。不关它就永远占着一条连接。
4. **读响应体**：`io.ReadAll(resp.Body)` 一次性读成 `[]byte`。`HTTP` 传的是字节流，要变成人能看的字符串还得知道编码，所以标准做法是 `string(body)`。
5. **看状态码**：`resp.StatusCode`。判断成功看这个 `int`（`2xx`），**不要**拿 `resp.Status` 那个字符串（`"200 OK"`）去比较。

把这些步骤对应回前面的报文，是这样：

| 报文里的东西 | `Go` 里对应谁 |
| --- | --- |
| `GET /users HTTP/1.1` | `req.Method`（`"GET"`）、`req.URL`（`/users`） |
| `Host: jsonplaceholder.typicode.com` | 拼在 `http.NewRequest` 的第 2 个参数里 |
| `Content-Type: application/json` | `req.Header.Set("Content-Type", "application/json")` |
| `{"title":"My Post"}`（只有 `POST` 才有） | `http.NewRequest` 的第 3 个参数 |
| `HTTP/1.1 200 OK` | `resp.StatusCode`（`200`）、`resp.Status`（`"200 OK"`） |
| `Content-Type: application/json; charset=utf-8` | `resp.Header.Get("Content-Type")` |
| `Location: https://jsonplaceholder.typicode.com/posts/101`（只有 `201` 才有） | `resp.Header.Get("Location")` |
| 响应体的 `[{"id":1,...}]` | `io.ReadAll(resp.Body)` |

对照着看这两张图更直观：

![HTTP请求报文构成](images/HTTP请求报文.png)

![HTTP响应报文构成](images/HTTP响应报文.png)

## `net/http` 三件套

在我们实际使用 Go 语言的`net/http`包发送HTTP请求前，我们需要了解`net/http`包中的一些基本概念。

`net/http` 里真正会用到的只有三个类型，分工很清晰：`Client` 负责“**怎么发**”，`Request` 负责“**发什么**”，`Response` 负责“**收到了什么**”。

### `http.Client`：怎么发

你可以把它理解成一个“会发请求的工人”。它**零值就能用**（`&http.Client{}` 不写任何字段也是合法的），但你几乎总该给它设一个超时：

```go
client := &http.Client{
    Timeout: 10 * time.Second, // 一次完整请求的总超时：从建连接一直到读完响应体
}
```

不设超时的后果很严重：对端一直不响应，程序就会**无限期挂住**。在生产环境里，“卡住不返回”往往比“直接报错”更难排查。

除了 `Timeout`，还有几个字段值得知道它存在，但**默认值通常就够了**：

| 字段 | 作用 | 你什么时候需要管它 |
| --- | --- | --- |
| `Timeout` | 一次完整请求的总超时 | **每次都该设** |
| `Transport` | 底层连接池、代理、`TLS` 配置 | 高并发时要调（见 `src/usersync`） |
| `Jar` | `Cookie` 存储 | 要让客户端“记住登录态”时才用 |
| `CheckRedirect` | 控制重定向行为 | 默认最多自动跟随 10 次，一般不用管 |

#### 关于 `Jar` 和 `CookieJar`

`Jar` 的类型是 `CookieJar`，它是一个**接口**：

```go
type CookieJar interface {
    SetCookies(u *url.URL, cookies []*Cookie)
    Cookies(u *url.URL) []*Cookie
}
```

它的作用是“给这个 `Client` 配一个存 `Cookie` 的地方”。

这里有个**新手必踩的坑**：`Jar` 的零值是 `nil`，而 `nil` 意味着“**不存也不带 `Cookie`**”。所以“复用同一个 `http.Client` 就会自动携带 `Cookie`”这句话，**只在你自己给它配了 `Jar` 之后才成立**。

标准库自带了一个基于内存的实现，在 `net/http/cookiejar` 包里：

```go
jar, _ := cookiejar.New(nil) // 纯内存实现，进程一重启就没了
client := &http.Client{
    Jar:     jar, // 配上它，后续请求才会自动带 Cookie
    Timeout: 10 * time.Second,
}
```

这也正是“浏览器登录一次就一直登录，而自己写的 Go 程序不会”的原因 —— 浏览器天生带着一个 `Jar`，`http.Client` 默认不配。

### `http.Request`：发什么

```go
req, err := http.NewRequest(http.MethodGet, target, nil)
```

`http.NewRequest` 的三个参数，正好对应报文的三个部分：方法、地址、请求体。最后传 `nil` 表示“没有请求体”（`GET` 就没有）。

还有一个 `http.NewRequestWithContext`，比它多一个 `context` 参数，用来做**级联取消和超时控制**（比如按下 `Ctrl+C` 时，让所有在途请求立刻返回）。什么时候才需要它？在有并发、有成串调用的时候 —— 见 `src/usersync`；像 `naive-get` 这样的单请求示例用不上。

#### `Header`：请求头

`req.Header` 的类型是 `http.Header`，本质是一个 `map[string][]string` —— 键是头字段名，值是一个字符串切片。

为什么值要用切片？因为同一个头字段**允许出现多次**（比如 `Set-Cookie`）。也正因为它是 `map`，标准库提供了几个比直接操作 `map` 更安全的便捷方法：

| 方法 | 作用 | 例子 |
| --- | --- | --- |
| `Set(key, value)` | 设置，**覆盖**已有的同名头 | `req.Header.Set("Content-Type", "application/json")` |
| `Add(key, value)` | 追加一个值（同名头会有多个值） | `req.Header.Add("X-Trace-Id", id)` |
| `Get(key)` | 取第一个值 | `req.Header.Get("Authorization")` |
| `Del(key)` | 删除 | —— |

注意 `Set` 和 `Add` 的区别：发 `Content-Type` 要用 `Set`（一个请求只能有一个内容类型），加“追踪 id”这类可以重复的头才用 `Add`。

#### `Body`：请求体

`Body` 字段的类型是 `io.ReadCloser`，但我们**不需要自己去构造一个 `io.ReadCloser`** —— `http.NewRequest` 的第 3 个参数类型是 `io.Reader`，只要是“能读出一串字节”的东西都行：

```go
req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(postData))
```

（`io.Reader` 是个很宽的接口：`bytes.NewReader`、`strings.NewReader`、一个打开的文件、一个网络连接，都满足它。如果你传进去的东西本来不是 `io.ReadCloser`，标准库会自动帮你包一层。）

实际用得最多的就两个：`bytes.NewReader(data)`（已经有了 `[]byte`）和 `strings.NewReader(s)`（已经有了字符串）。

那 `io.ReadCloser` 是什么？它只是把两个接口**内嵌**在一起：

```go
type ReadCloser interface {
    Reader // 能读
    Closer // 用完要关
}
```

`Body` 声明成 `io.ReadCloser`，是因为发完之后标准库需要把它关掉。顺便说，`Response.Body` 也是这个类型 —— 这就是为什么它**必须 `Close`**。

### `http.Response`：收到了什么

```go
type Response struct {
    Status     string // "200 OK"  —— 给人看的
    StatusCode int    // 200       —— 给程序判断用的
    Header     Header // 响应头，同样是 map[string][]string
    Body       io.ReadCloser
}
```

| 字段 | 你会怎么用它 |
| --- | --- |
| `StatusCode` | **判断成败就看它**：`if resp.StatusCode == 200`，或者判断整个 `2xx` 区间 |
| `Status` | `"200 OK"`，打印日志时好看，**别拿它做判断**（多一个空格、改个大小写就崩了） |
| `Header` | `resp.Header.Get("Content-Type")` 看服务端给的是什么类型 |
| `Body` | `io.ReadAll(resp.Body)` 读成 `[]byte` —— 但记得先 `defer resp.Body.Close()` |

关于 `Body` 要强调一句：它**必须关闭**。它是一条连接上的数据流，不是已经读进内存的字节；不 `Close`，连接就永远不会归还给连接池，程序会持续泄漏连接和内存。固定套路就是“拿到 `resp` 的第一件事，就是 `defer resp.Body.Close()`”。

还有一个小陷阱：`io.ReadAll` 会**不管多大都往内存里读**。如果对端返回 1GB，你的程序就吃掉 1GB。所以工程代码里会给它套一个上限（`io.LimitReader`），见 `src/usersync`。

## 动手试试

下面 4 个练习都只需要**改一行**，改完 `go run .` 就能看到现象。先 `cd src/naive-get`。

1. **换个接口**：把 `src/naive-get/main.go` 里的 `const target` 改成 `https://jsonplaceholder.typicode.com/users/1`（只取第一个用户）。
   *你应该看到*：返回的 `JSON` 从数组 `[...]` 变成了单个对象 `{...}` —— 路径变了，取到的东西就变了。

2. **发一次 `DELETE`**：把 `http.MethodGet` 改成 `http.MethodDelete`，地址改成 `https://jsonplaceholder.typicode.com/posts/1`。
   *你应该看到*：`200`，加上一个空对象 `{}`。很多真实接口对 `DELETE` 返回的是 `204 No Content`（没有响应体），这时 `io.ReadAll` 读到的是空字节 —— 这正是速查表里 `204` 那一行的意思。

3. **打印响应头**：把 `naive-get` 里那行 `printHeader(resp)` 的注释去掉。
   *你应该看到*：一堆响应头，其中包括 `Content-Type: application/json; charset=utf-8`。回头看看上面那两张报文图 —— 你打印出来的就是图里的东西。

4. **故意请求一个不存在的路径**：把 `const target` 改成 `https://jsonplaceholder.typicode.com/userz`（注意是 `userz`，故意拼错）。
   *你应该看到*：程序**不会报错**，`err` 是 `nil`，但 `resp.StatusCode` 是 `404`，响应体是 `{}`。
   这一点很重要：**“请求失败”和“响应是错误状态码”是两件事**。网络层成功了、业务层失败了，Go 会照常把响应交给你 —— 判断状态码是你的活。

做完这 4 个练习，你已经可以对照着读 `src/usersync` 了。

## 接下来去哪

- 想先把“发一个请求”写到能跑：看 `src/naive-get`、`src/naive-post`，那里面的注释是逐行的。
- 想知道“真实工程里这些代码长什么样”：看 `src/usersync`，以及它的 [`README.md`](../src/usersync/README.md) —— 那里有一张和 `naive` 版本逐条对照的表，还列了 5 条进阶建议（`errgroup`、接口抽象、用 `httptest` 模拟 `429` 等等）。
- 这份文档讲的是 `HTTP` 本身。下一步是**框架**（`gin`、`kratos`）、**数据库**、**并发** —— 这些我们会在社团里慢慢聊。
