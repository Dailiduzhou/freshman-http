// 这是「用 Go 发一个 HTTP GET 请求」的最小可运行示例。
//
// 运行方式（先 cd 到 src/naive-get 目录）：
//
//	go run .                                 # 直接编译并运行
//	go build -o naive-get . && ./naive-get   # 先编译成可执行文件，再运行
//
// 同目录下的 go.mod 声明了模块名和 Go 版本，它让这个目录成为一个独立的项目。
// 想看「真实工程里会怎么改写这份代码」，可以对照 src/usersync。

// package main 是 Go 里唯一能编译出可执行程序的包名。
// 一个目录就是一个包，该目录下所有 .go 文件的第一行都必须是同一个包名。
package main

// import 引入需要用到的包。标准库的包直接写名字即可，
// 不需要像第三方库那样先在 go.mod 里声明。
// 这里的三个包分别负责：读字节流(io)、发 HTTP 请求(net/http)、表示时间(time)。
//
// 注意：Go 把「导入却没用上」的包直接当作编译错误，所以不会留下无用的 import。
import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// func main 是程序入口：程序启动后从这里的第一行开始执行。
// 它不能有参数，也不能有返回值；需要指定退出码时得用 os.Exit。
func main() {
	// const 声明编译期常量，赋值后不能再改。
	// 把地址单独拎出来，是为了将来换接口时只改这一处。
	const target = "https://jsonplaceholder.typicode.com/users"

	// http.Client 是 HTTP 客户端，负责建连接、发请求、收响应。
	// 它是并发安全的，一个进程里通常只创建一个，这样可以复用底层连接。
	//
	// & 是取地址符：这里得到的是一个 *http.Client（指向结构体的指针），
	// 而不是结构体的副本。Go 里结构体一般都用指针传递，避免整份复制。
	//
	// Timeout 指的是「一次完整请求」的总超时（从建连到读完响应体）。
	// 不设超时的后果很严重：对端一直不响应，程序就会无限期挂住。
	// 在生产环境里，「卡住不返回」往往比「直接报错」更难排查。
	client := &http.Client{Timeout: time.Second * 10}

	// Go 的招牌写法：函数可以有多个返回值，最后一个通常用来返回 error。
	// 这里 http.NewRequest 返回 (*http.Request, error)：请求对象 + 错误。
	//
	// 第三个参数是请求体：GET 请求没有 body，所以传 nil（表示「空/没有」）。
	//
	// http.MethodGet 是标准库提供的常量，值为 "GET"，
	// 比手写字符串更不容易拼错（同理还有 MethodPost、MethodPut 等）。
	//
	// := 是「声明并赋值」，左边两个变量 req、err 都是新变量，
	// 类型由右边的返回值自动推导，不用手写 *http.Request。
	req, err := http.NewRequest(http.MethodGet, target, nil)
	// Go 的错误处理惯例：错误是一种普通的值，用返回值逐层传递，而不是抛异常。
	// 所以每次调用之后都要自己检查 err != nil。啰嗦，但流程完全显式。
	//
	// panic 会让程序立刻崩溃并打印调用栈。它适合「已经无法继续」的场景，
	// 这里为了让示例足够短才这么写；真实项目更常见的做法是把 error 往上层返回，
	// 由调用方决定是重试、降级还是退出（工程版见 src/usersync）。
	if err != nil {
		panic(err)
	}

	// client.Do 真正把请求发出去，返回 (*http.Response, error)。
	// 这里 err 名字重复出现，但因为左边有全新的变量 resp，所以还能继续用 :=。
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}

	// defer 的作用：把这行调用推迟到「当前函数即将返回时」执行。
	// 无论函数是正常返回还是中途 panic，defer 都会执行。
	// 所以「拿到需要手动释放的资源，立刻 defer 释放」是 Go 里最常用的固定套路。
	//
	// 为什么必须关？resp.Body 是连接上的数据流，不是已经读到内存的字节。
	// 不关闭，连接就永远不会归还给连接池，程序会持续泄漏连接和内存。
	defer resp.Body.Close()

	// 把响应体一次性读完。
	// 返回的 body 类型是 []byte（字节切片）而不是 string：
	// HTTP 传的是字节流，要变成字符串还得知道它用的字符编码。
	//
	// 注意这里的 err 是新变量（:=），覆盖了作用域内之前那个已经不用的 err。
	// 这在 Go 里是日常操作：err 就像个反复出现的「信号灯」。
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	// 想看响应头长什么样，把下面这行的注释去掉即可。（函数定义在文件末尾）
	// printHeader(resp)

	// string(body) 是一次显式类型转换：[]byte 转成 string 才能按文本打印。
	fmt.Println(string(body))
}

// printHeader 遍历并打印所有响应头。
//
// 函数名首字母小写 → 只在当前包内可见；首字母大写才是对外导出的（相当于 public）。
// 参数类型写成 *http.Response（指针），因为我们只读它的内容，
// 没必要把整个响应结构体复制一份。
func printHeader(resp *http.Response) {
	// resp.Header 的实际类型是 http.Header，
	// 它本质上是 map[string][]string：键是头字段名，值是字符串切片。
	//
	// 为什么值要用切片？因为同一个头字段允许出现多次，比如 Set-Cookie。
	//
	// for range 遍历 map 时，两个变量的含义是「键, 值」。
	// 要特别注意：Go 里 map 的遍历顺序是随机的，不要依赖它的输出顺序。
	for key, values := range resp.Header {
		// values 是 []string，再来一层 for range 逐个取元素。
		// 这里用不到下标，就用 _ 占位——Go 不允许声明了却没用到的变量。
		for _, value := range values {
			// 这里用标准库的 fmt.Println（写到标准输出 stdout，正式代码用它）。
			// Go 里还有一个内建的 println，那是给调试用的：它写到标准错误(stderr)，
			// 输出格式也不保证稳定，所以不要留在正式代码里。
			fmt.Println(key, value)
		}
	}
}
